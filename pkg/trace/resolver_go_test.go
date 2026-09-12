package trace

import (
	"context"
	"os"
	"testing"

	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
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

// TestFilterByModulePath_EscapedLastElement covers module paths whose last
// element the Go linker escapes in symbol names (objabi.PathToPrefix): the
// root package of gopkg.in/yaml.v3 appears as "gopkg.in/yaml%2ev3.Unmarshal",
// while subpackages keep the unescaped module prefix.
func TestFilterByModulePath_EscapedLastElement(t *testing.T) {
	tests := []struct {
		modPath string
		entries []FunctionEntry
		want    []string
	}{
		{
			modPath: "gopkg.in/yaml.v3",
			entries: []FunctionEntry{
				{Name: "gopkg.in/yaml%2ev3.Unmarshal", Offset: 0x1000},
				{Name: "gopkg.in/yaml%2ev3.(*Decoder).Decode", Offset: 0x1100},
				{Name: "gopkg.in/yaml.v3.Unmarshal", Offset: 0x1200},
				{Name: "gopkg.in/yaml.v3/internal/parser.Parse", Offset: 0x2000},
				{Name: "gopkg.in/yaml%2ev2.Unmarshal", Offset: 0x3000},
				{Name: "gopkg.in/yaml.v3x.Fake", Offset: 0x3100},
				{Name: "main.main", Offset: 0x4000},
				{Name: "runtime.goexit", Offset: 0x5000},
			},
			want: []string{
				"gopkg.in/yaml%2ev3.Unmarshal",
				"gopkg.in/yaml%2ev3.(*Decoder).Decode",
				"gopkg.in/yaml.v3.Unmarshal",
				"gopkg.in/yaml.v3/internal/parser.Parse",
				"main.main",
			},
		},
		{
			modPath: "example.com/foo.bar",
			entries: []FunctionEntry{
				{Name: "example.com/foo%2ebar.Run", Offset: 0x1000},
				{Name: "example.com/foo.bar/cmd.Exec", Offset: 0x2000},
				{Name: "example.com/foo%2ebaz.Other", Offset: 0x3000},
				{Name: "example.com/foo.Root", Offset: 0x3100},
				{Name: "main.main", Offset: 0x4000},
			},
			want: []string{
				"example.com/foo%2ebar.Run",
				"example.com/foo.bar/cmd.Exec",
				"main.main",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.modPath, func(t *testing.T) {
			got := filterByModulePath(tt.entries, tt.modPath)
			var names []string
			for _, e := range got {
				names = append(names, e.Name)
			}
			require.ElementsMatch(t, tt.want, names)
		})
	}
}

func TestGoPathToPrefix(t *testing.T) {
	tests := map[string]string{
		"github.com/user/repo":  "github.com/user/repo",
		"gopkg.in/yaml.v3":      "gopkg.in/yaml%2ev3",
		"example.com/foo.bar":   "example.com/foo%2ebar",
		"example.com/a.b/c.d.e": "example.com/a.b/c%2ed%2ee",
		"yaml.v3":               "yaml%2ev3",
		"example.com/100%/x":    "example.com/100%25/x",
		"example.com/sp ace":    "example.com/sp%20ace",
		"example.com/q\"uote":   "example.com/q%22uote",
		"example.com/caf\u00e9": "example.com/caf%c3%a9",
	}
	for in, want := range tests {
		require.Equal(t, want, goPathToPrefix(in), "input %q", in)
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

// TestGoProjectResolver_NonexistentBinary verifies that a missing file yields
// an error that is NOT ErrProjectScopeUnsupported and still carries *os.PathError
// in its chain. Reverting the *os.PathError guard in goModulePath would break this.
func TestGoProjectResolver_NonexistentBinary(t *testing.T) {
	resolver := GoProjectResolver("/nonexistent-binary-path", log.Nop(), "", "", nil, nil)
	_, err := resolver(context.Background())
	if err == nil {
		t.Fatal("expected error for nonexistent binary, got nil")
	}
	if errors.Is(err, ErrProjectScopeUnsupported) {
		t.Errorf("nonexistent path should not be ErrProjectScopeUnsupported, got: %v", err)
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Errorf("expected *os.PathError in error chain, got: %v", err)
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
