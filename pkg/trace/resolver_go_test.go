package trace

import (
	"testing"
)

func TestFilterByModulePath(t *testing.T) {
	entries := []FunctionEntry{
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
