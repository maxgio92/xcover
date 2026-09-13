package trace

import "github.com/ianlancetaylor/demangle"

// Symbol names come from the traced ELF and are untrusted input, so both the
// demangler's input and its output are bounded.
//
// maxMangledLen caps the input at 16 KiB. Neither the Itanium nor the Rust v0
// parser bounds its recursion depth, so a long run of nested prefixes overflows
// the goroutine stack, which is a fatal runtime error rather than a recoverable
// panic. The Itanium parser is also quadratic in input length. Real
// template-heavy C++ names rarely exceed a few KiB; longer names pass through
// unchanged.
//
// maxDemangledLenPow bounds the output to 2^16 bytes. Rust v0 back references
// re-render the referenced substring each time they are hit, so a crafted
// symbol a few hundred bytes long can otherwise expand to gigabytes.
const (
	maxMangledLen      = 1 << 14
	maxDemangledLenPow = 16
)

// demangleName returns the human-readable form of a mangled C++ (Itanium ABI)
// or Rust (legacy and v0) symbol name. Names that are not mangled, such as C
// and Go symbols or the synthetic func_0x<addr> names, are returned unchanged,
// as are names longer than maxMangledLen.
//
// No format options are passed on purpose: parameter types and template
// arguments stay in the output, so C++ overloads and template instantiations
// remain distinct in filters and reports. The only option is the output length
// cap, which does not alter names that stay within it.
func demangleName(raw string) string {
	if len(raw) > maxMangledLen {
		return raw
	}
	return demangle.Filter(raw, demangle.MaxLength(maxDemangledLenPow))
}
