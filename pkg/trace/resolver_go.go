package trace

import (
	"context"
	"debug/buildinfo"
	"debug/elf"
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
			return nil, errors.Wrapf(ErrProjectScopeUnsupported, "failed to detect Go module path: %v", err)
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
func goModulePath(path string) (string, error) {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return "", err
	}
	if info.Path == "command-line-arguments" {
		return "", errors.New("binary was built as command-line-arguments; build the package or module instead")
	}
	if info.Main.Path == "" {
		return "", errors.New("binary has no main module path")
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
func filterByModulePath(entries []FunctionEntry, modPath string) []FunctionEntry {
	prefix := modPath + "/"
	var out []FunctionEntry
	for _, e := range entries {
		if strings.HasPrefix(e.Name, prefix) || strings.HasPrefix(e.Name, modPath+".") || strings.HasPrefix(e.Name, "main.") {
			out = append(out, e)
		}
	}
	return out
}
