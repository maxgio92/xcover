// Package agent provides the "xcover agent" subcommand, which exposes
// bpftime agent library management operations.
package agent

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/maxgio92/xcover/pkg/bpftime"
	"github.com/maxgio92/xcover/pkg/cmd/options"
)

const CmdName = "agent"

// NewCommand returns the "xcover agent" cobra command and its subcommands.
func NewCommand(opts *options.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               CmdName,
		Short:             "Manage the bpftime agent library (experimental)",
		DisableAutoGenTag: true,
	}

	cmd.AddCommand(newExtractCommand(opts))

	return cmd
}

type extractOptions struct {
	*options.Options
}

func newExtractCommand(opts *options.Options) *cobra.Command {
	o := &extractOptions{Options: opts}

	cmd := &cobra.Command{
		Use:   "extract",
		Short: "Extract the bpftime agent library and print its path",
		Long: `Extract the embedded bpftime agent library to a temporary file and print
its absolute path.

The printed path is intended for use as LD_PRELOAD in the tracee environment
before starting the program under test:

  export LD_PRELOAD=$(xcover agent extract)
  ./my-binary

The temporary file persists until the system cleans it up or the user removes
it manually.`,
		DisableAutoGenTag: true,
		RunE:              o.run,
	}

	return cmd
}

func (o *extractOptions) run(_ *cobra.Command, _ []string) error {
	path, err := bpftime.ExtractAgent()
	if err != nil {
		return err
	}

	fmt.Println(path)

	return nil
}
