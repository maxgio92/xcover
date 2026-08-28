package stop

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/common"
	"github.com/maxgio92/xcover/pkg/cmd/options"
)

var (
	ErrNotRunningOrNotFound = errors.Errorf("%s not running or PID file not found", settings.CmdName)
	ErrInvalidPIDFile       = errors.New("invalid PID file")
	ErrProcessNotFound      = errors.New("process not found")
	ErrFailedToStop         = errors.Errorf("failed to stop %s", settings.CmdName)
)

type Options struct {
	*options.Options
}

func NewCommand(opts *options.Options) *cobra.Command {
	o := &Options{opts}

	cmd := &cobra.Command{
		Use:               "stop",
		Short:             fmt.Sprintf("Stop the %s profiler daemon", settings.CmdName),
		DisableAutoGenTag: true,
		SilenceUsage:      true,
		RunE:              o.Run,
	}

	return cmd
}

func (o *Options) Run(cmd *cobra.Command, _ []string) error {
	pid, err := common.ReadPID()
	if err != nil {
		if errors.Is(err, common.ErrInvalidPID) {
			return ErrInvalidPIDFile
		}

		return ErrNotRunningOrNotFound
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return ErrProcessNotFound
	}

	err = process.Signal(syscall.SIGTERM)
	if err != nil {
		return ErrFailedToStop
	}

	// Wait for process to stop.
	for i := 0; i < 50; i++ {
		if !common.IsDaemonRunning() {
			fmt.Printf("%s stopped (PID %d)\n", settings.CmdName, pid)
			common.RemovePID()

			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Force kill if still running.
	process.Kill()
	common.RemovePID()
	fmt.Printf("%s force killed (PID %d)\n", settings.CmdName, pid)

	return nil
}
