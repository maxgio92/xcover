package trace

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// note encodes one ELF note (Elf_Nhdr followed by 4-byte padded name and
// descriptor), as laid out by the System V ABI and read by parseBuildIDNote.
func note(namesz, descsz, ntype uint32, name, desc []byte) []byte {
	b := make([]byte, 12)
	binary.LittleEndian.PutUint32(b[0:], namesz)
	binary.LittleEndian.PutUint32(b[4:], descsz)
	binary.LittleEndian.PutUint32(b[8:], ntype)
	b = append(b, name...)
	b = append(b, make([]byte, (4-len(name)%4)%4)...)
	b = append(b, desc...)
	b = append(b, make([]byte, (4-len(desc)%4)%4)...)
	return b
}

func TestParseBuildIDNote(t *testing.T) {
	gnu := []byte("GNU\x00")
	id := []byte{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06,
		0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	valid := note(4, uint32(len(id)), ntGNUBuildID, gnu, id)

	tests := []struct {
		name string
		data []byte
		want []byte
	}{
		{"valid GNU note", valid, id},
		{"empty payload", nil, nil},
		{"namesz wraps align4", note(0xffffffff, 4, ntGNUBuildID, nil, []byte{1, 2, 3, 4}), nil},
		{"descsz larger than payload", note(4, 1<<20, ntGNUBuildID, gnu, id), nil},
		{"truncated descriptor", valid[:len(valid)-4], nil},
		{"wrong owner with build-id type then GNU note", append(note(4, 4, ntGNUBuildID, []byte("XYZ\x00"), []byte{9, 9, 9, 9}), valid...), id},
		{"namesz shorter than GNU with GNU padding bytes", note(2, 4, ntGNUBuildID, gnu, []byte{1, 2, 3, 4}), nil},
		{"odd descsz keeps unpadded length", note(4, 3, ntGNUBuildID, gnu, []byte{1, 2, 3}), []byte{1, 2, 3}},
		// GNU ld does not pad the final descriptor: `-Wl,--build-id=0x010203`
		// yields a 19-byte .note.gnu.build-id (namesz=4, descsz=3).
		{"unpadded final descriptor", note(4, 3, ntGNUBuildID, gnu, []byte{1, 2, 3})[:19], []byte{1, 2, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, parseBuildIDNote(tt.data, binary.LittleEndian))
		})
	}
}

// patchNoteFilesz copies the ELF64 little-endian file at src into dir with
// the p_filesz of its first PT_NOTE program header set to filesz, and
// returns the copy's path. The header is patched in the raw bytes so the
// value bypasses debug/elf, which does not check p_filesz against the file
// length.
func patchNoteFilesz(t *testing.T, src, dir string, filesz uint64) string {
	t.Helper()
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	require.Equal(t, elf.ELFCLASS64, elf.Class(data[elf.EI_CLASS]))
	require.Equal(t, elf.ELFDATA2LSB, elf.Data(data[elf.EI_DATA]))

	// Elf64_Ehdr: e_phoff at 0x20, e_phentsize at 0x36, e_phnum at 0x38.
	// Elf64_Phdr: p_type at 0, p_filesz at 32.
	phoff := binary.LittleEndian.Uint64(data[0x20:])
	phentsize := uint64(binary.LittleEndian.Uint16(data[0x36:]))
	phnum := uint64(binary.LittleEndian.Uint16(data[0x38:]))
	for i := uint64(0); i < phnum; i++ {
		ph := data[phoff+i*phentsize:]
		if elf.ProgType(binary.LittleEndian.Uint32(ph)) != elf.PT_NOTE {
			continue
		}
		binary.LittleEndian.PutUint64(ph[32:], filesz)
		dst := filepath.Join(dir, filepath.Base(src))
		require.NoError(t, os.WriteFile(dst, data, 0o755))
		return dst
	}
	t.Fatalf("no PT_NOTE program header in %s", src)
	return ""
}

// TestBuildID_OversizedNoteSegment patches the fixture's PT_NOTE p_filesz to
// sizes that debug/elf accepts but that no real note segment has. The size
// comes from the traced binary, so buildID must skip the segment rather than
// size an allocation by it. The fixture keeps its section table, so the
// build-id still comes from the .note.gnu.build-id fallback.
func TestBuildID_OversizedNoteSegment(t *testing.T) {
	for _, filesz := range []uint64{1 << 40, 1 << 62} {
		t.Run(fmt.Sprintf("filesz=%#x", filesz), func(t *testing.T) {
			path := patchNoteFilesz(t, "testdata/gotest", t.TempDir(), filesz)
			f, err := elf.Open(path)
			require.NoError(t, err)
			defer f.Close()

			require.Equal(t, testGotestBuildID, hex.EncodeToString(buildID(f)))
		})
	}
}

// recordingReaderAt serves data and counts the calls, so a test can assert
// that buildID never reads a segment it must skip.
type recordingReaderAt struct {
	data  []byte
	calls int
}

func (r *recordingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	r.calls++
	return bytes.NewReader(r.data).ReadAt(p, off)
}

// TestBuildID_NoteSegmentBound pins maxNoteSegmentSize on a synthetic file:
// a PT_NOTE exactly at the cap is read and parsed, one byte over it is
// skipped before any read. The fixture test above covers the elf.Open path
// but cannot tell a 64 KiB cap from a much looser one.
func TestBuildID_NoteSegmentBound(t *testing.T) {
	gnu := []byte("GNU\x00")
	id := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	payload := note(4, uint32(len(id)), ntGNUBuildID, gnu, id)
	payload = append(payload, make([]byte, maxNoteSegmentSize-len(payload))...)

	newFile := func(filesz uint64, r io.ReaderAt) *elf.File {
		return &elf.File{
			FileHeader: elf.FileHeader{ByteOrder: binary.LittleEndian},
			Progs: []*elf.Prog{{
				ProgHeader: elf.ProgHeader{Type: elf.PT_NOTE, Filesz: filesz},
				ReaderAt:   r,
			}},
		}
	}

	t.Run("at the cap", func(t *testing.T) {
		r := &recordingReaderAt{data: payload}
		require.Equal(t, id, buildID(newFile(maxNoteSegmentSize, r)))
		require.Equal(t, 1, r.calls)
	})
	t.Run("one byte over the cap", func(t *testing.T) {
		r := &recordingReaderAt{data: payload}
		require.Nil(t, buildID(newFile(maxNoteSegmentSize+1, r)))
		require.Zero(t, r.calls, "buildID read a segment it must skip")
	})
}
