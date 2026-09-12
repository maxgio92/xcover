package trace

import (
	"context"
	"debug/buildinfo"
	"debug/elf"
	"fmt"
	"os"
	"strings"

	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
)

// GoProjectResolver returns a FunctionResolver that resolves only functions
// belonging to the Go module that produced the binary. Standard library
// functions and third-party dependencies are excluded.
//
// It reads the embedded Go build info to discover the module path, then
// delegates to SymbolTableResolver and filters the results.
//
// Returns ErrProjectScopeUnsupported (via errors.Is) if the binary does not
// carry the metadata needed for project-scoped resolution (missing build info,
// built as command-line-arguments, etc.).
func GoProjectResolver(path string, logger log.Logger, include, exclude string, bindInclude, bindExclude []elf.SymBind) FunctionResolver {
	return func(ctx context.Context) ([]FunctionEntry, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		modPath, err := goModulePath(path)
		if err != nil {
			return nil, errors.Wrap(err, "failed to detect Go module path")
		}

		logger.Info().
			Str("module", modPath).
			Msg("project scope: filtering functions to module")

		// Resolve all functions first, then filter.
		all := SymbolTableResolver(path, logger, include, exclude, bindInclude, bindExclude)
		entries, err := all(ctx)
		if err != nil {
			return nil, err
		}

		filtered := filterByModulePath(entries, modPath)
		if len(filtered) == 0 {
			return nil, errors.Errorf("no functions found for module %q", modPath)
		}

		logger.Info().
			Int("total", len(entries)).
			Int("project", len(filtered)).
			Msg("project scope: filtered functions")

		return filtered, nil
	}
}

// goModulePath extracts the Go module path from the binary's embedded build info.
//
// Returns ErrProjectScopeUnsupported (via errors.Is) in three situations:
//   - buildinfo.ReadFile failed for a non-filesystem reason (no build info,
//     corrupt ELF, non-Go binary)
//   - binary was built as command-line-arguments
//   - binary has an empty main module path
//
// File-system errors (*os.PathError) are returned as-is so callers can
// distinguish "file unreadable" from "project scope not supported".
func goModulePath(path string) (string, error) {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			return "", err
		}
		// Binary exists but has no .go.buildinfo section (non-Go binary).
		return "", errors.Wrapf(ErrProjectScopeUnsupported, "no Go build info: %s", err)
	}
	if info.Path == "command-line-arguments" {
		return "", errors.Wrap(ErrProjectScopeUnsupported, "binary was built as command-line-arguments; build the package or module instead")
	}
	if info.Main.Path == "" {
		return "", errors.Wrap(ErrProjectScopeUnsupported, "binary has no main module path")
	}
	return info.Main.Path, nil
}

// withProjectFallback wraps primary so that if it returns ErrProjectScopeUnsupported
// the error is logged as a warning and fallback is used instead. Any other error
// from primary is returned as-is.
func withProjectFallback(primary, fallback FunctionResolver, logger log.Logger) FunctionResolver {
	return func(ctx context.Context) ([]FunctionEntry, error) {
		entries, err := primary(ctx)
		if err == nil {
			return entries, nil
		}
		if !errors.Is(err, ErrProjectScopeUnsupported) {
			return nil, err
		}
		logger.Warn().
			Err(err).
			Msg("project scope unavailable, falling back to binary scope")
		return fallback(ctx)
	}
}

// filterByModulePath keeps FunctionEntry values belonging to the main module.
// Go emits symbols for subpackages with the module path prefix
// (e.g. "github.com/user/repo/pkg.Func"), but symbols in the executable's
// root package are emitted as "main.Func".
//
// The linker escapes the last path element of a package path in symbol names
// (see goPathToPrefix), so a module such as gopkg.in/yaml.v3 emits its root
// package functions as "gopkg.in/yaml%2ev3.Unmarshal" while build info reports
// "gopkg.in/yaml.v3". Both the escaped and the unescaped forms are matched.
func filterByModulePath(entries []FunctionEntry, modPath string) []FunctionEntry {
	prefixes := []string{"main.", modPath + "/", modPath + "."}
	if escaped := goPathToPrefix(modPath); escaped != modPath {
		prefixes = append(prefixes, escaped+"/", escaped+".")
	}
	var out []FunctionEntry
	for _, e := range entries {
		for _, p := range prefixes {
			if strings.HasPrefix(e.Name, p) {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

// goPathToPrefix mirrors cmd/internal/objabi.PathToPrefix: the symbol-name
// prefix the Go linker derives from a package path. Dots in the last path
// element, '%', '"', control characters, spaces and non-ASCII bytes are
// escaped as %xx so that the '.' separating package from identifier stays
// unambiguous.
func goPathToPrefix(s string) string {
	slash := strings.LastIndex(s, "/")
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= ' ' || (c == '.' && i > slash) || c == '%' || c == '"' || c >= 0x7F {
			fmt.Fprintf(&b, "%%%02x", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}
