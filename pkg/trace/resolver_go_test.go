package trace

import (
	"context"
	"testing"

	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
)

func TestFilterByModulePath(t *testing.T) {
	entries := []FunctionEntry{
		{Name: "main.main", Offset: 0x0800},
		{Name: "main.local", Offset: 0x0900},
		{Name: "github.com/user/repo/pkg.Foo", Offset: 0x1000},
		{Name: "github.com/user/repo.Main", Offset: 0x2000},
		{Name: "github.com/user/repo/internal/bar.Baz", Offset: 0x3000},
		{Name: "runtime.goexit", Offset: 0x4000},
		{Name: "net/http.ListenAndServe", Offset: 0x5000},
		{Name: "github.com/other/dep.Helper", Offset: 0x6000},
		{Name: "github.com/user/repofork.Fake", Offset: 0x7000},
	}

	got := filterByModulePath(entries, "github.com/user/repo")

	want := map[string]bool{
		"main.main":                             true,
		"main.local":                            true,
		"github.com/user/repo/pkg.Foo":          true,
		"github.com/user/repo.Main":             true,
		"github.com/user/repo/internal/bar.Baz": true,
	}

	if len(got) != len(want) {
		t.Fatalf("filterByModulePath returned %d entries, want %d", len(got), len(want))
	}

	for _, e := range got {
		if !want[e.Name] {
			t.Errorf("unexpected entry: %s", e.Name)
		}
	}
}

func TestFilterByModulePath_NoMatch(t *testing.T) {
	entries := []FunctionEntry{
		{Name: "runtime.goexit", Offset: 0x1000},
		{Name: "net/http.ListenAndServe", Offset: 0x2000},
	}

	got := filterByModulePath(entries, "github.com/user/repo")
	if len(got) != 0 {
		t.Errorf("expected empty result, got %d entries", len(got))
	}
}

func staticResolver(entries []FunctionEntry, err error) FunctionResolver {
	return func(_ context.Context) ([]FunctionEntry, error) {
		return entries, err
	}
}

func TestWithProjectFallback_UsesPrimaryOnSuccess(t *testing.T) {
	primary := staticResolver([]FunctionEntry{{Name: "main.Foo", Offset: 0x1000}}, nil)
	fallback := staticResolver([]FunctionEntry{{Name: "other.Bar", Offset: 0x2000}}, nil)

	resolver := withProjectFallback(primary, fallback, log.Nop())
	got, err := resolver(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "main.Foo" {
		t.Errorf("expected primary result, got %v", got)
	}
}

func TestWithProjectFallback_FallsBackOnUnsupported(t *testing.T) {
	primary := staticResolver(nil, errors.Wrap(ErrProjectScopeUnsupported, "no buildinfo"))
	fallback := staticResolver([]FunctionEntry{{Name: "other.Bar", Offset: 0x2000}}, nil)

	resolver := withProjectFallback(primary, fallback, log.Nop())
	got, err := resolver(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "other.Bar" {
		t.Errorf("expected fallback result, got %v", got)
	}
}

func TestWithProjectFallback_PropagatesOtherErrors(t *testing.T) {
	sentinel := errors.New("some other failure")
	primary := staticResolver(nil, sentinel)
	fallback := staticResolver([]FunctionEntry{{Name: "other.Bar", Offset: 0x2000}}, nil)

	resolver := withProjectFallback(primary, fallback, log.Nop())
	_, err := resolver(context.Background())
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}
