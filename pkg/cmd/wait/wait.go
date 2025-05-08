package wait

import (
	"fmt"
	"github.com/maxgio92/xcover/pkg/healthcheck"
	log "github.com/rs/zerolog"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/trace"
)

const CmdName = "wait"

func NewCommand(o *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               CmdName,
		Short:             fmt.Sprintf("Wait for the %s profiler to be ready", settings.CmdName),
		DisableAutoGenTag: true,
		RunE:              o.Run,
	}

	cmd.Flags().StringVarP(&o.socketPath, "socket-path", "s", trace.HealthCheckSockPath, fmt.Sprintf("Path to the %s socket file", settings.CmdName))
	cmd.Flags().DurationVar(&o.timeout, "timeout", time.Second*120, "Timeout")

	return cmd
}

func (o *Options) Run(cmd *cobra.Command, _ []string) error {
	var err error
	o.LogLevel, err = cmd.Flags().GetString("log-level")
	if err != nil {
		return errors.Wrap(err, "failed to get log level")
	}

	logLevel, err := log.ParseLevel(o.LogLevel)
	if err != nil {
		o.Logger.Fatal().Err(err).Msg("invalid log level")
	}
	o.Logger = o.Logger.Level(logLevel).With().Str("component", "wait").Logger()

	start := time.Now()
	retryInterval := 500 * time.Millisecond
	o.Logger.Info().Msg("waiting for the profiler to be ready")

	for {
		if time.Since(start) >= o.timeout {
			return errors.New("timeout waiting for profiler readiness")
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

		if buf[0] == healthcheck.ReadyMsg {
			o.Logger.Info().Msg("profiler is ready")
			return nil
		}

		time.Sleep(retryInterval)
	}
}
