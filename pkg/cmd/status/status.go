package status

import (
	"fmt"
	"os"

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
		Short:             fmt.Sprintf("Check the the %s profiler status", settings.CmdName),
		DisableAutoGenTag: true,
		SilenceUsage:      true,
		Run:               o.Run,
	}

	return cmd
}

func (o *Options) Run(cmd *cobra.Command, _ []string) {
	if common.IsDaemonRunning() {
		pidData, _ := os.ReadFile(settings.PidFile)
		fmt.Printf("%s is running (PID %s)\n", settings.CmdName, pidData)
	} else {
		fmt.Printf("%s is not running\n", settings.CmdName)
	}
}
