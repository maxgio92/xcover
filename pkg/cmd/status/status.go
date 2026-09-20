package status

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/common"
	"github.com/maxgio92/xcover/pkg/cmd/options"
)

type Options struct {
	*options.Options
}

func NewCommand(opts *options.Options) *cobra.Command {
	o := &Options{opts}
	cmd := &cobra.Command{
		Use:               "status",
		Short:             fmt.Sprintf("Check the %s profiler status", settings.CmdName),
		DisableAutoGenTag: true,
		SilenceUsage:      true,
		RunE:              o.Run,
	}

	return cmd
}

func (o *Options) Run(cmd *cobra.Command, _ []string) error {
	pid, err := common.CheckRunning()
	if err != nil {
		fmt.Println(common.ErrNotRunning.Error())

		return nil
	}
	fmt.Printf("%s is running (PID %d)\n", settings.CmdName, pid)

	return nil
}
