package merge

import (
	"fmt"
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/maxgio92/xcover/pkg/cmd/options"
	"github.com/maxgio92/xcover/pkg/coverage"
)

const CmdName = "merge"

type Options struct {
	output string

	*options.Options
}

func NewCommand(opts *options.Options) *cobra.Command {
	o := &Options{Options: opts}
	cmd := &cobra.Command{
		Use:   fmt.Sprintf("%s <report.json> [report.json...]", CmdName),
		Short: "Merge multiple coverage reports into one",
		Long: `
merge combines coverage reports produced by separate runs (parallel shards,
distributed E2E, retries) into a single aggregate report.

Reports are merged by function name: a function counts as covered if it was
acknowledged in any input. Coverage is recomputed over the union. The inputs
must come from the same binary; a merge spanning different binaries is reported
as a warning and the resulting exe_path is left empty.`,
		Args:              cobra.MinimumNArgs(1),
		DisableAutoGenTag: true,
		SilenceUsage:      true,
		RunE:              o.Run,
	}

	cmd.Flags().StringVarP(&o.output, "output", "o", "", "Write the merged report to this file instead of stdout")

	return cmd
}

func (o *Options) Run(_ *cobra.Command, args []string) error {
	reports := make([]*coverage.CoverageReport, 0, len(args))
	for _, path := range args {
		f, err := os.Open(path)
		if err != nil {
			return errors.Wrapf(err, "failed to open report %q", path)
		}

		report, err := coverage.ReadReport(f)
		f.Close()
		if err != nil {
			return errors.Wrapf(err, "failed to parse report %q", path)
		}

		reports = append(reports, report)
	}

	if paths := coverage.ExePaths(reports...); len(paths) > 1 {
		o.Logger.Warn().
			Strs("exe_paths", paths).
			Msg("merging reports from different binaries; coverage may be meaningless and exe_path is left empty")
	}

	merged, err := coverage.Merge(reports...)
	if err != nil {
		return errors.Wrap(err, "failed to merge reports")
	}

	out := os.Stdout
	if o.output != "" {
		f, err := os.Create(o.output)
		if err != nil {
			return errors.Wrapf(err, "failed to create output file %q", o.output)
		}
		defer f.Close()
		out = f
	}

	if err := merged.WriteReport(out); err != nil {
		return errors.Wrap(err, "failed to write merged report")
	}

	return nil
}
