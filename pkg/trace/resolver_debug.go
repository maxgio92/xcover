package trace

import (
	"bytes"
	"context"
	"debug/dwarf"
	"debug/elf"
	"encoding/binary"

	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
)

// ntGNUBuildID is the ELF note type for a GNU build-id (NT_GNU_BUILD_ID).
const ntGNUBuildID = 3

// SeparateDebugResolver returns a FunctionResolver that reads function symbols
// from a companion debug file (e.g. `objcopy --only-keep-debug` output, a
// distro -dbg package, or a debuginfod artifact) while computing uprobe attach
// offsets against the executable that is actually mmap'd at runtime.
//
// Symbols come from the debug file's .symtab (primary) or DWARF subprograms
// (fallback). Offsets are computed from the executable's PT_LOAD program
// headers: function virtual addresses are identical in both files because they
// come from the same link, and program headers survive even section-table
// stripping (e.g. `eu-strip --strip-sections`) where .symtab/.dynsym/.eh_frame
// are gone and no other resolver can work.
//
// build-id verification (read from PT_NOTE, so it works on section-stripped
// executables) guards against pairing a debug file with the wrong executable.
// Unless skipBuildID is set, a missing or mismatched build-id fails resolution.
//
// On failure the returned error is deliberately NOT wrapped in ErrNoSymbolTable,
// so UserTracee.Init surfaces it instead of silently falling back to binary
// recovery: when the user explicitly supplies a debug file, a problem with it
// should be reported, not papered over with synthetic func_0x<addr> names.
func SeparateDebugResolver(exePath, debugPath string, logger log.Logger, include, exclude string, bindInclude, bindExclude []elf.SymBind, skipBuildID bool) FunctionResolver {
	return func(ctx context.Context) ([]FunctionEntry, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		exe, err := elf.Open(exePath)
		if err != nil {
			return nil, errors.Wrap(err, "failed to open executable")
		}
		defer exe.Close()

		dbg, err := elf.Open(debugPath)
		if err != nil {
			return nil, errors.Wrap(err, "failed to open debug file")
		}
		defer dbg.Close()

		if err := verifyBuildIDMatch(exe, dbg, skipBuildID, logger); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Offsets always come from the executable, never the debug file: the
		// debug file's loadable sections are SHT_NOBITS and its file offsets are
		// meaningless, but its symbol values are the (shared) virtual addresses.
		toOffset := func(va uint64) (uint64, error) { return vaToFileOffset(exe, va) }

		// Primary: the debug file's .symtab (retained by --only-keep-debug and
		// `eu-strip -f`). Names here are already linkage-level and unambiguous.
		syms, err := funcSymsFromELF(dbg, include, exclude, bindInclude, bindExclude)
		if err == nil {
			if syms = definedFuncs(syms); len(syms) > 0 {
				logger.Info().Int("symbols", len(syms)).Msg("resolved functions from debug file .symtab")
				return funcEntriesFromSymbols(syms, toOffset, logger)
			}
		}

		// Fallback: DWARF subprograms. Best-effort — see funcSymsFromDWARF.
		logger.Info().Msg("debug file has no usable .symtab; trying DWARF subprograms")
		dwarfSyms, derr := funcSymsFromDWARF(dbg, include, exclude, logger)
		if derr != nil {
			return nil, errors.Wrapf(derr, "no usable symbols in debug file %q (.symtab and DWARF both failed)", debugPath)
		}
		if dwarfSyms = definedFuncs(dwarfSyms); len(dwarfSyms) == 0 {
			return nil, errors.Errorf("no defined functions in debug file %q", debugPath)
		}
		return funcEntriesFromSymbols(dwarfSyms, toOffset, logger)
	}
}

// definedFuncs drops undefined and zero-address symbols. Imported functions
// (e.g. printf) appear in .symtab as STT_FUNC with SHN_UNDEF and Value==0; on a
// PIE the first PT_LOAD has Vaddr 0, so VA 0 maps to file offset 0 (the ELF
// header) instead of being rejected, which would attach a bogus probe.
func definedFuncs(syms []elf.Symbol) []elf.Symbol {
	out := make([]elf.Symbol, 0, len(syms))
	for _, s := range syms {
		if s.Section == elf.SHN_UNDEF || s.Value == 0 {
			continue
		}
		out = append(out, s)
	}
	return out
}

// dwarfSubprogram holds the name attributes of a DW_TAG_subprogram DIE, plus a
// reference to follow when the name lives on another DIE.
type dwarfSubprogram struct {
	name    string
	linkage string
	ref     dwarf.Offset // DW_AT_specification / DW_AT_abstract_origin target
	hasRef  bool
}

// funcSymsFromDWARF extracts function entries from DWARF .debug_info as a
// fallback when the debug file has no .symtab.
//
// Only subprograms with a code address are emitted (declarations and
// abstract/inlined instances are skipped). The entry address follows DWARF 5
// (§2.18, §3.3.5): DW_AT_entry_pc, else DW_AT_low_pc, else the first
// DW_AT_ranges entry. DW_AT_linkage_name is preferred
// over DW_AT_name so C++ overloads stay distinct. For C++ member and namespaced
// functions the definition DIE carries DW_AT_low_pc but its name only via a
// DW_AT_specification/DW_AT_abstract_origin reference to the declaration, so
// those references are followed (a pre-pass indexes DIE names by offset).
//
// Known limitations (both reasons .symtab — retained by --only-keep-debug and
// `eu-strip -f` — is the primary, far more robust source):
//   - split-DWARF (-gsplit-dwarf) keeps subprogram DIEs in separate .dwo files
//     not present here, so few functions resolve.
//   - debug files that move data into a supplementary object file — dwz's
//     default GNU extension (.gnu_debugaltlink, DW_FORM_GNU_ref_alt/strp_alt)
//     or the DWARF 5 standard equivalent (.debug_sup, DW_FORM_ref_sup4/8 and
//     strp_sup; §7.3.6) — reference names cross-file. Go's debug/dwarf decodes
//     these forms to raw offsets but never loads the supplementary file, so
//     such names resolve empty and are skipped. Common on Fedora/Debian
//     debuginfod, which dwz-process their debug files.
func funcSymsFromDWARF(f *elf.File, include, exclude string, logger log.Logger) ([]elf.Symbol, error) {
	d, err := f.DWARF()
	if err != nil {
		return nil, errors.Wrap(err, "no DWARF info")
	}
	if f.Section(".gnu_debugaltlink") != nil {
		logger.Warn().Msg("debug file uses a dwz supplementary file (.gnu_debugaltlink); names referenced via DW_FORM_GNU_ref_alt cannot be resolved and will be skipped")
	}

	// Pre-pass: index every subprogram DIE's names by offset so references can
	// be resolved.
	byOffset := map[dwarf.Offset]dwarfSubprogram{}
	r := d.Reader()
	for {
		entry, err := r.Next()
		if err != nil {
			return nil, errors.Wrap(err, "reading DWARF")
		}
		if entry == nil {
			break
		}
		if entry.Tag != dwarf.TagSubprogram {
			continue
		}
		sp := dwarfSubprogram{}
		sp.name, _ = entry.Val(dwarf.AttrName).(string)
		sp.linkage, _ = entry.Val(dwarf.AttrLinkageName).(string)
		if off, ok := entry.Val(dwarf.AttrSpecification).(dwarf.Offset); ok {
			sp.ref, sp.hasRef = off, true
		} else if off, ok := entry.Val(dwarf.AttrAbstractOrigin).(dwarf.Offset); ok {
			sp.ref, sp.hasRef = off, true
		}
		byOffset[entry.Offset] = sp
	}

	// resolveName follows specification/abstract_origin references (bounded) to
	// find a linkage or plain name.
	resolveName := func(sp dwarfSubprogram) string {
		for hops := 0; hops < 8; hops++ {
			if sp.linkage != "" {
				return sp.linkage
			}
			if sp.name != "" {
				return sp.name
			}
			if !sp.hasRef {
				return ""
			}
			next, ok := byOffset[sp.ref]
			if !ok {
				return ""
			}
			sp = next
		}
		return ""
	}

	var syms []elf.Symbol
	r = d.Reader()
	for {
		entry, err := r.Next()
		if err != nil {
			return nil, errors.Wrap(err, "reading DWARF")
		}
		if entry == nil {
			break
		}
		if entry.Tag != dwarf.TagSubprogram {
			continue
		}
		// Entry address per DWARF 5 (§2.18, §3.3.5): prefer DW_AT_entry_pc;
		// otherwise DW_AT_low_pc (which for a non-contiguous function may be a
		// base address rather than the entry); otherwise the first DW_AT_ranges
		// entry. A subprogram with none of these is a declaration/abstract
		// instance with no code, and is skipped.
		addr, ok := entry.Val(dwarf.AttrEntrypc).(uint64)
		if !ok || addr == 0 {
			addr, ok = entry.Val(dwarf.AttrLowpc).(uint64)
		}
		if !ok || addr == 0 {
			if rs, rerr := d.Ranges(entry); rerr == nil && len(rs) > 0 {
				addr = rs[0][0]
			}
		}
		if addr == 0 {
			continue
		}
		name := resolveName(byOffset[entry.Offset])
		if name == "" {
			continue
		}
		sym := elf.Symbol{Name: name, Value: addr, Info: byte(elf.STT_FUNC)}
		if shouldInclude(sym, include, exclude, nil, nil) {
			syms = append(syms, sym)
		}
	}
	if len(syms) == 0 {
		return nil, errors.New("no DWARF subprograms with a concrete low_pc")
	}
	logger.Info().Int("subprograms", len(syms)).Msg("resolved functions from DWARF")
	return syms, nil
}

// verifyBuildIDMatch fails unless the executable and debug file carry the same
// GNU build-id. The id is read from PT_NOTE program headers rather than the
// .note.gnu.build-id section so that verification still works on executables
// whose section table has been stripped.
func verifyBuildIDMatch(exe, dbg *elf.File, skip bool, logger log.Logger) error {
	if skip {
		logger.Warn().Msg("build-id verification disabled (--no-build-id-check)")
		return nil
	}
	exeID := buildID(exe)
	dbgID := buildID(dbg)
	if len(exeID) == 0 || len(dbgID) == 0 {
		return errors.Wrap(ErrNoBuildID, "cannot verify the debug file belongs to the executable; pass --no-build-id-check to bypass")
	}
	if !bytes.Equal(exeID, dbgID) {
		return errors.Wrapf(ErrDebugBuildIDMismatch, "executable=%x debug=%x", exeID, dbgID)
	}
	logger.Debug().Hex("build_id", exeID).Msg("debug file build-id matches executable")
	return nil
}

// buildID returns the GNU build-id from the first PT_NOTE segment that contains
// one, or nil if absent. Reading from the program header (not the section)
// means it survives section-table stripping.
func buildID(f *elf.File) []byte {
	for _, p := range f.Progs {
		if p.Type != elf.PT_NOTE {
			continue
		}
		data := make([]byte, p.Filesz)
		if _, err := p.ReadAt(data, 0); err != nil {
			continue
		}
		if id := parseBuildIDNote(data, f.ByteOrder); id != nil {
			return id
		}
	}
	return nil
}

// parseBuildIDNote scans a PT_NOTE payload for an NT_GNU_BUILD_ID note and
// returns its descriptor (the build-id bytes), or nil.
func parseBuildIDNote(data []byte, bo binary.ByteOrder) []byte {
	r := bytes.NewReader(data)
	for r.Len() >= 12 {
		var namesz, descsz, ntype uint32
		if err := binary.Read(r, bo, &namesz); err != nil {
			return nil
		}
		if err := binary.Read(r, bo, &descsz); err != nil {
			return nil
		}
		if err := binary.Read(r, bo, &ntype); err != nil {
			return nil
		}
		name := make([]byte, align4(namesz))
		if _, err := r.Read(name); err != nil {
			return nil
		}
		desc := make([]byte, align4(descsz))
		if _, err := r.Read(desc); err != nil {
			return nil
		}
		if ntype == ntGNUBuildID && namesz >= 3 && string(name[:3]) == "GNU" {
			return desc[:descsz]
		}
	}
	return nil
}

// align4 rounds n up to the next 4-byte boundary (ELF note fields are padded).
func align4(n uint32) uint32 { return (n + 3) &^ 3 }
