package wait

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/trace"
)

func NewCommand(o *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "wait",
		Short:             fmt.Sprintf("Wait for the %s profiler to be ready", settings.CmdName),
		DisableAutoGenTag: true,
		RunE:              o.Run,
	}

	cmd.Flags().StringVarP(&o.socketPath, "socket-path", "s", trace.HealthCheckSockPath, fmt.Sprintf("Path to the %s socket file", settings.CmdName))
	cmd.Flags().DurationVar(&o.timeout, "timeout", time.Second*120, "Timeout")

	return cmd
}

func (o *Options) Run(_ *cobra.Command, _ []string) error {
	start := time.Now()
	retryInterval := 500 * time.Millisecond
	o.Logger.Info().Msgf("waiting for %s to be ready", settings.CmdName)

	for {
		if time.Since(start) >= o.timeout {
			return errors.New("timeout waiting for xcover readiness")
		}

		// Check if socket exists.
		info, err := os.Stat(o.socketPath)
		if err != nil {
			if os.IsNotExist(err) {
				time.Sleep(retryInterval)
				continue
			}
			return fmt.Errorf("error checking socket: %w", err)
		}

		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("path exists but is not a Unix socket: %s", o.socketPath)
		}

		// Try to connect.
		conn, err := net.DialTimeout("unix", o.socketPath, retryInterval)
		if err != nil {
			if errors.Is(err, syscall.EACCES) {
				return errors.Wrap(err, "failed connecting")
			}
			time.Sleep(retryInterval)
			continue
		}

		defer conn.Close()

		// Try reading one byte.
		buf := make([]byte, 1)
		conn.SetReadDeadline(time.Now().Add(retryInterval))

		n, err := conn.Read(buf)
		if err != nil || n == 0 {
			time.Sleep(retryInterval)
			continue
		}

		if buf[0] == 0x01 {
			o.Logger.Info().Msgf("%s is ready", settings.CmdName)
			return nil
		}

		time.Sleep(retryInterval)
	}
}
