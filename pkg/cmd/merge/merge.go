package merge

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/maxgio92/xcover/pkg/cmd/options"
	"github.com/maxgio92/xcover/pkg/coverage"
)

const CmdName = "merge"

// stdinPath is the argument that selects standard input as a report source.
const stdinPath = "-"

type Options struct {
	output                 string
	allowMismatchedBuildID bool
	*options.Options
}

func NewCommand(opts *options.Options) *cobra.Command {
	o := &Options{Options: opts}
	cmd := &cobra.Command{
		Use:   fmt.Sprintf("%s [flags] <report.json>...", CmdName),
		Short: "Merge coverage reports of the same binary into one",
		Long: fmt.Sprintf(`
%s combines coverage reports produced by separate runs (parallel shards,
distributed E2E, retries) into a single aggregate report.

Functions are identified by their offset within the binary named by build_id:
a function counts as covered if it was hit in any input, and cov_by_func is
recomputed over the merged set. Every input must carry the same build_id;
reports with a different or empty build_id are refused unless
--allow-mismatched-build-id is set, in which case the merged report has an
empty build_id.

Pass '%s' as a path to read one report from standard input. The merged report is
written to standard output unless --output is set.`, CmdName, stdinPath),
		Args:              cobra.MinimumNArgs(1),
		DisableAutoGenTag: true,
		SilenceUsage:      true,
		RunE:              o.Run,
	}

	cmd.Flags().StringVarP(&o.output, "output", "o", "", "Write the merged report to this file instead of stdout")
	cmd.Flags().BoolVar(&o.allowMismatchedBuildID, "allow-mismatched-build-id", false, "Merge reports whose build_id differs or is empty; the result has an empty build_id")

	return cmd
}

func (o *Options) Run(cmd *cobra.Command, args []string) error {
	o.Logger = o.Logger.With().Str("component", CmdName).Logger()

	reports := make([]*coverage.CoverageReport, 0, len(args))
	for _, path := range args {
		report, err := o.readReport(cmd.InOrStdin(), path)
		if err != nil {
			return err
		}
		reports = append(reports, report)
	}

	var mergeOpts []coverage.MergeOption
	if o.allowMismatchedBuildID {
		mergeOpts = append(mergeOpts, coverage.WithAllowMismatchedBuildID())
	}

	merged, err := coverage.Merge(reports, mergeOpts...)
	if err != nil {
		return errors.Wrap(err, "failed to merge reports")
	}
	o.Logger.Info().
		Int("reports", len(reports)).
		Int("funcs_traced", len(merged.FuncsTraced)).
		Int("funcs_ack", len(merged.FuncsAck)).
		Float64("cov_by_func", merged.CovByFunc).
		Msg("reports merged")

	if o.output == "" {
		return errors.Wrap(merged.WriteReport(cmd.OutOrStdout()), "failed to write merged report")
	}

	return errors.Wrapf(writeFile(o.output, merged), "failed to write merged report to %q", o.output)
}

func (o *Options) readReport(stdin io.Reader, path string) (*coverage.CoverageReport, error) {
	r := stdin
	if path != stdinPath {
		f, err := os.Open(path)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to open report %q", path)
		}
		defer f.Close()
		r = f
	}

	report, err := coverage.ReadReport(r)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read report %q", path)
	}
	o.Logger.Debug().Str("path", path).Str("build_id", report.BuildID).Msg("report read")

	return report, nil
}

// writeFile writes the report through a temporary file in the target
// directory and renames it into place, so an existing file at path is left
// untouched if writing fails.
func writeFile(path string, report *coverage.CoverageReport) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return errors.Wrap(err, "failed to create temporary file")
	}
	defer os.Remove(tmp.Name())

	if err := report.WriteReport(tmp); err != nil {
		tmp.Close()
		return err
	}
	// CreateTemp opens the file 0600; match the mode xcover run uses for its
	// report so a later unprivileged step can read a merge written as root.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return errors.Wrap(err, "failed to set temporary file mode")
	}
	if err := tmp.Close(); err != nil {
		return errors.Wrap(err, "failed to close temporary file")
	}

	return errors.Wrap(os.Rename(tmp.Name(), path), "failed to rename temporary file")
}
