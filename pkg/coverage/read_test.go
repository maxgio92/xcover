package coverage_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/coverage"
)

// validReport mirrors what pkg/trace buildReport writes.
const validReport = `{
  "schema_version": 1,
  "xcover_version": "v1.2.3",
  "generated_at": "2026-01-02T03:04:05Z",
  "kernel": "6.1.0",
  "exe_path": "/bin/app",
  "build_id": "abc",
  "funcs_traced": ["bar", "foo"],
  "funcs_ack": ["foo"],
  "cov_by_func": 50,
  "functions": [
    {"name": "foo", "offset": 16, "hit": true},
    {"name": "bar", "offset": 32, "hit": false}
  ]
}`

func TestReadReport(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:  "valid report",
			input: validReport,
		},
		{
			name:  "trailing whitespace is fine",
			input: validReport + "\n\n",
		},
		{
			name:  "zero traced functions",
			input: `{"schema_version":1,"build_id":"abc","funcs_traced":[],"funcs_ack":[],"cov_by_func":0,"functions":[]}`,
		},
		{
			name:  "unknown fields are ignored within the same schema",
			input: `{"schema_version":1,"build_id":"abc","functions":[],"extra":true}`,
		},
		{
			name:    "empty input",
			input:   ``,
			wantErr: "failed to decode",
		},
		{
			name:    "null document",
			input:   `null`,
			wantErr: "schema_version",
		},
		{
			name:    "empty object",
			input:   `{}`,
			wantErr: "schema_version",
		},
		{
			name:    "truncated document",
			input:   validReport[:len(validReport)/2],
			wantErr: "failed to decode",
		},
		{
			name:    "trailing document",
			input:   validReport + validReport,
			wantErr: "trailing data",
		},
		{
			name:    "trailing garbage",
			input:   validReport + " x",
			wantErr: "trailing data",
		},
		{
			name:    "missing schema_version",
			input:   `{"build_id":"abc","functions":[]}`,
			wantErr: "schema_version 0",
		},
		{
			name:    "unsupported schema_version",
			input:   `{"schema_version":2,"build_id":"abc","functions":[]}`,
			wantErr: "schema_version 2",
		},
		{
			name:    "missing functions",
			input:   `{"schema_version":1,"build_id":"abc","funcs_traced":[],"funcs_ack":[]}`,
			wantErr: "missing functions",
		},
		{
			name:    "ack not a subset of traced",
			input:   `{"schema_version":1,"build_id":"abc","funcs_traced":["foo"],"funcs_ack":["foo","baz"],"functions":[{"name":"foo","offset":1,"hit":true}]}`,
			wantErr: `"baz" is not in funcs_traced`,
		},
		{
			name:    "ack repeated more often than traced",
			input:   `{"schema_version":1,"build_id":"abc","funcs_traced":["foo"],"funcs_ack":["foo","foo"],"functions":[{"name":"foo","offset":1,"hit":true}]}`,
			wantErr: `"foo" is not in funcs_traced`,
		},
		{
			name:    "two names at one offset",
			input:   `{"schema_version":1,"build_id":"abc","funcs_traced":["bar","foo"],"funcs_ack":[],"functions":[{"name":"foo","offset":1,"hit":false},{"name":"bar","offset":1,"hit":false}]}`,
			wantErr: "offset 1 listed twice",
		},
		{
			name:    "null function record",
			input:   `{"schema_version":1,"build_id":"abc","functions":[null]}`,
			wantErr: "has no name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := coverage.ReadReport(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				require.Nil(t, report)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, report)
			require.Equal(t, coverage.SchemaVersion, report.SchemaVersion)
			require.Equal(t, "abc", report.BuildID)
		})
	}
}

func TestReadReportRoundTrip(t *testing.T) {
	report, err := coverage.ReadReport(strings.NewReader(validReport))
	require.NoError(t, err)

	want := coverage.NewCoverageReport(
		coverage.WithReportXcoverVersion("v1.2.3"),
		coverage.WithReportGeneratedAt("2026-01-02T03:04:05Z"),
		coverage.WithReportKernel("6.1.0"),
		coverage.WithReportExePath("/bin/app"),
		coverage.WithReportBuildID("abc"),
		coverage.WithReportFuncsTraced([]string{"bar", "foo"}),
		coverage.WithReportFuncsAck([]string{"foo"}),
		coverage.WithReportFuncsCov(50),
		coverage.WithReportFunctions([]coverage.FunctionCoverage{
			{Name: "foo", Offset: 16, Hit: true},
			{Name: "bar", Offset: 32, Hit: false},
		}),
	)
	require.Equal(t, want, report)
}
