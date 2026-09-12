package coverage

import (
	"encoding/json"
	"io"

	"github.com/pkg/errors"
)

// ReadReport decodes exactly one report document from r and validates it
// against the schema this build understands. It fails closed: an unsupported
// or missing schema_version, an empty or null document, trailing data after
// the document, a funcs_ack entry that is not in funcs_traced, or two
// function names at the same offset are all rejected.
func ReadReport(r io.Reader) (*CoverageReport, error) {
	// Decode into a bare struct, not NewCoverageReport, so a missing
	// schema_version stays zero instead of inheriting the current default.
	var report CoverageReport

	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&report); err != nil {
		return nil, errors.Wrap(err, "failed to decode report")
	}
	if err := decoder.Decode(&json.RawMessage{}); err != io.EOF {
		if err == nil {
			return nil, errors.New("trailing data after report document")
		}
		return nil, errors.Wrap(err, "trailing data after report document")
	}

	if err := validateReport(&report); err != nil {
		return nil, errors.Wrap(err, "invalid report")
	}

	return &report, nil
}

func validateReport(report *CoverageReport) error {
	if report.SchemaVersion != SchemaVersion {
		return errors.Errorf("unsupported or missing schema_version %d, want %d", report.SchemaVersion, SchemaVersion)
	}

	if report.Functions == nil {
		return errors.New("missing functions")
	}

	names := make(map[uint64]string, len(report.Functions))
	for _, fn := range report.Functions {
		if fn.Name == "" {
			return errors.Errorf("function at offset %d has no name", fn.Offset)
		}
		if prev, ok := names[fn.Offset]; ok {
			return errors.Errorf("offset %d listed twice, as %q and %q", fn.Offset, prev, fn.Name)
		}
		names[fn.Offset] = fn.Name
	}

	traced := make(map[string]int, len(report.FuncsTraced))
	for _, name := range report.FuncsTraced {
		traced[name]++
	}
	for _, name := range report.FuncsAck {
		if traced[name] == 0 {
			return errors.Errorf("funcs_ack entry %q is not in funcs_traced", name)
		}
		traced[name]--
	}

	return nil
}
