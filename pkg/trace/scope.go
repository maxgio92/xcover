package trace

import "fmt"

// Scope controls which functions are resolved from a binary for tracing.
type Scope string

const (
	// ScopeBinary traces all functions in the binary. This is the default.
	ScopeBinary Scope = "binary"

	// ScopeProject traces only functions belonging to the project's own
	// module, excluding standard library and third-party dependencies.
	// Currently supported for Go binaries only.
	ScopeProject Scope = "project"
)

// ParseScope parses a string into a Scope, returning an error for unknown values.
func ParseScope(s string) (Scope, error) {
	switch Scope(s) {
	case ScopeBinary, ScopeProject:
		return Scope(s), nil
	default:
		return "", fmt.Errorf("unknown scope %q: must be %q or %q", s, ScopeBinary, ScopeProject)
	}
}
