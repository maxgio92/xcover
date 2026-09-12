package merge_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	log "github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/cmd/merge"
	"github.com/maxgio92/xcover/pkg/cmd/options"
	"github.com/maxgio92/xcover/pkg/coverage"
)

const (
	reportA = `{"schema_version":1,"build_id":"abc","exe_path":"/bin/app","funcs_traced":["bar","foo"],"funcs_ack":["foo"],"cov_by_func":50,
"functions":[{"name":"foo","offset":1,"hit":true},{"name":"bar","offset":2,"hit":false}]}`
	reportB = `{"schema_version":1,"build_id":"abc","exe_path":"/bin/app","funcs_traced":["bar","foo"],"funcs_ack":["bar"],"cov_by_func":50,
"functions":[{"name":"foo","offset":1,"hit":false},{"name":"bar","offset":2,"hit":true}]}`
	reportOtherBuild = `{"schema_version":1,"build_id":"def","functions":[{"name":"foo","offset":1,"hit":true}]}`
)

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func run(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	opts := options.NewOptions(
		options.WithContext(context.Background()),
		options.WithLogger(log.New(&stderr)),
	)
	cmd := merge.NewCommand(opts)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)

	err := cmd.Execute()
	require.True(t, stdout.Len() == 0 || strings.HasPrefix(stdout.String(), "{"), "stdout must only carry JSON")

	return stdout.String(), err
}

func TestMergeCommand(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.json", reportA)
	b := writeTemp(t, dir, "b.json", reportB)
	other := writeTemp(t, dir, "other.json", reportOtherBuild)

	tests := []struct {
		name      string
		args      []string
		stdin     string
		wantErr   string
		wantAck   []string
		wantBuild string
	}{
		{
			name:    "no arguments",
			args:    []string{},
			wantErr: "requires at least 1 arg",
		},
		{
			name:    "missing input file",
			args:    []string{filepath.Join(dir, "missing.json")},
			wantErr: "failed to open report",
		},
		{
			name:    "invalid input file",
			args:    []string{writeTemp(t, dir, "bad.json", "{}")},
			wantErr: "failed to read report",
		},
		{
			name:    "mismatched build_id",
			args:    []string{a, other},
			wantErr: "build_id mismatch",
		},
		{
			name:      "mismatched build_id with override",
			args:      []string{"--allow-mismatched-build-id", a, other},
			wantAck:   []string{"foo"},
			wantBuild: "",
		},
		{
			name:      "two files to stdout",
			args:      []string{a, b},
			wantAck:   []string{"bar", "foo"},
			wantBuild: "abc",
		},
		{
			name:      "stdin and file",
			args:      []string{"-", b},
			stdin:     reportA,
			wantAck:   []string{"bar", "foo"},
			wantBuild: "abc",
		},
		{
			name:    "empty stdin",
			args:    []string{"-"},
			stdin:   "",
			wantErr: `failed to read report "-"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := run(t, tt.stdin, tt.args...)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				require.Empty(t, out)
				return
			}
			require.NoError(t, err)

			merged, err := coverage.ReadReport(strings.NewReader(out))
			require.NoError(t, err)
			require.Equal(t, tt.wantAck, merged.FuncsAck)
			require.Equal(t, tt.wantBuild, merged.BuildID)
		})
	}
}

func TestMergeCommandOutputFile(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.json", reportA)
	b := writeTemp(t, dir, "b.json", reportB)
	target := filepath.Join(dir, "merged.json")

	out, err := run(t, "", "-o", target, a, b)
	require.NoError(t, err)
	require.Empty(t, out)

	f, err := os.Open(target)
	require.NoError(t, err)
	defer f.Close()
	merged, err := coverage.ReadReport(f)
	require.NoError(t, err)
	require.Equal(t, []string{"bar", "foo"}, merged.FuncsAck)
	require.Equal(t, float64(100), merged.CovByFunc)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 3, "no temporary file left behind")

	info, err := os.Stat(target)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "merged report must be world-readable like xcover run's")
}

func TestMergeCommandOutputFilePreservedOnFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	dir := t.TempDir()
	a := writeTemp(t, dir, "a.json", reportA)
	outDir := filepath.Join(dir, "out")
	require.NoError(t, os.Mkdir(outDir, 0o755))
	target := writeTemp(t, outDir, "merged.json", "keep me")
	require.NoError(t, os.Chmod(outDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(outDir, 0o755) })

	_, err := run(t, "", "-o", target, a)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to write merged report")

	content, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "keep me", string(content))
}
