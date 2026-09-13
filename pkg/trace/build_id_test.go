package trace

import (
	"encoding/binary"
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
