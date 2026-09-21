package wait

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/maxgio92/xcover/pkg/cmd/common"
	"github.com/maxgio92/xcover/pkg/cmd/options"
	"github.com/maxgio92/xcover/pkg/healthcheck"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/trace"
)

const CmdName = "wait"

var (
	ErrNotRunning = common.ErrNotRunning
	ErrExited     = errors.Errorf("%s exited before becoming ready", settings.CmdName)
	ErrTimeout    = errors.New("timeout waiting for profiler readiness")
)

type Options struct {
	socketPath string
	timeout    time.Duration
	// retryInterval paces the poll loop and bounds each dial and read.
	// Zero means 500 ms; tests inject a shorter one.
	retryInterval time.Duration
	// errOut receives the daemon log tail before an error return. Nil
	// means os.Stderr; tests inject a buffer.
	errOut io.Writer
	*options.Options
}

func NewCommand(opts *options.Options) *cobra.Command {
	o := new(Options)
	o.Options = opts
	cmd := &cobra.Command{
		Use:               CmdName,
		Short:             fmt.Sprintf("Wait for the %s profiler to be ready", settings.CmdName),
		DisableAutoGenTag: true,
		SilenceUsage:      true,
		RunE:              o.Run,
	}

	cmd.Flags().StringVarP(&o.socketPath, "socket-path", "s", trace.HealthCheckSockPath, fmt.Sprintf("Path to the %s socket file", settings.CmdName))
	cmd.Flags().DurationVar(&o.timeout, "timeout", time.Second*120, "Timeout")

	return cmd
}

func (o *Options) Run(cmd *cobra.Command, _ []string) error {
	if o.errOut == nil {
		o.errOut = os.Stderr
	}
	if o.retryInterval == 0 {
		o.retryInterval = 500 * time.Millisecond
	}

	// Tests build Options without a context.
	ctx := o.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	o.Logger = o.Logger.With().Str("component", "wait").Logger()

	if _, err := common.CheckRunning(); err != nil {
		common.PrintLogTail(o.errOut)
		return err
	}

	start := time.Now()
	o.Logger.Info().Msg("waiting for the profiler to be ready")

	for {
		if time.Since(start) >= o.timeout {
			common.PrintLogTail(o.errOut)
			return ErrTimeout
		}

		// The daemon may fail after start-up (e.g. probe attach failure);
		// fail fast instead of polling a socket that will never become ready.
		if !common.IsDaemonRunning() {
			common.PrintLogTail(o.errOut)
			return ErrExited
		}

		ready, err := o.tryReady()
		if err != nil {
			return err
		}
		if ready {
			o.Logger.Info().Msg("profiler is ready")
			fmt.Printf("%s is ready\n", settings.CmdName)
			return nil
		}

		// Cancellation is the user's choice, not a daemon fault, so no log
		// tail is printed.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(o.retryInterval):
		}
	}
}

// tryReady makes one readiness attempt and closes its connection before
// returning, so the loop never holds more than one connection open. It
// reports false with a nil error for every outcome worth retrying and an
// error only for the ones that cannot recover.
func (o *Options) tryReady() (bool, error) {
	// Check if socket exists.
	info, err := os.Stat(o.socketPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errors.Wrap(err, "error checking socket")
	}

	if info.Mode()&os.ModeSocket == 0 {
		return false, errors.Errorf("path exists but is not a Unix socket: %s", o.socketPath)
	}

	// Try to connect.
	conn, err := net.DialTimeout("unix", o.socketPath, o.retryInterval)
	if err != nil {
		if errors.Is(err, syscall.EACCES) {
			return false, errors.Wrap(err, "failed connecting")
		}
		return false, nil
	}
	defer conn.Close()

	// Try reading one byte.
	buf := make([]byte, 1)
	conn.SetReadDeadline(time.Now().Add(o.retryInterval))

	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		return false, nil
	}

	return buf[0] == healthcheck.ReadyMsg, nil
}
