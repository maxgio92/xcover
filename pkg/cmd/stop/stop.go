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
	ErrForceKilled          = errors.Errorf("%s did not stop within the timeout and was force killed", settings.CmdName)
)

const (
	defaultTimeout = 30 * time.Second
	pollInterval   = 100 * time.Millisecond
)

type Options struct {
	timeout time.Duration
	*options.Options
}

func NewCommand(opts *options.Options) *cobra.Command {
	o := &Options{Options: opts}

	cmd := &cobra.Command{
		Use:               "stop",
		Short:             fmt.Sprintf("Stop the %s profiler daemon", settings.CmdName),
		DisableAutoGenTag: true,
		SilenceUsage:      true,
		RunE:              o.Run,
	}

	cmd.Flags().DurationVar(&o.timeout, "timeout", defaultTimeout, "Grace period to wait for the daemon to exit before force killing it")

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
	deadline := time.Now().Add(o.timeout)
	for {
		if !common.IsDaemonRunning() {
			fmt.Printf("%s stopped (PID %d)\n", settings.CmdName, pid)
			common.RemovePID()

			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(pollInterval)
	}

	// Force kill if still running. The daemon writes its report on SIGTERM,
	// so SIGKILL means the report may never have been produced.
	err = process.Kill()
	switch {
	case errors.Is(err, os.ErrProcessDone):
		// The daemon exited on its own during the last poll interval.
		fmt.Printf("%s stopped (PID %d)\n", settings.CmdName, pid)
		common.RemovePID()

		return nil
	case err != nil:
		// The daemon is still alive (e.g. EPERM), so keep the PID file.
		return errors.Wrapf(err, "failed to force kill %s (PID %d)", settings.CmdName, pid)
	}
	common.RemovePID()
	fmt.Printf("%s force killed (PID %d), the coverage report may be missing\n", settings.CmdName, pid)

	return ErrForceKilled
}
