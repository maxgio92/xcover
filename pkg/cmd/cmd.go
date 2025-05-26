package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	log "github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/options"
	"github.com/maxgio92/xcover/pkg/cmd/run"
	"github.com/maxgio92/xcover/pkg/cmd/status"
	"github.com/maxgio92/xcover/pkg/cmd/stop"
	"github.com/maxgio92/xcover/pkg/cmd/wait"
)

func NewCommand(o *options.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   settings.CmdName,
		Short: fmt.Sprintf("%s is a functional test coverage profiler", settings.CmdName),
		Long: fmt.Sprintf(`
%s is a functional test coverage profiler.

Run the '%s' command to run the profiler that will trace all the functions of the tracee program.
Wait for the profiler to be ready before running your tests, with the '%s' command.
Once the profiler is ready to trace all the functions, you can start running your tests.
At the end of your tests, the profiler can be stopped and a report being collected.
`,
			settings.CmdName, run.CmdName, wait.CmdName),
		DisableAutoGenTag: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			logLevelS, err := cmd.Flags().GetString("log-level")
			if err != nil {
				return fmt.Errorf("failed to read log-level: %w", err)
			}
			o.LogLevel = logLevelS

			logLevel, err := log.ParseLevel(o.LogLevel)
			if err != nil {
				o.Logger.Fatal().Err(err).Msg("invalid log level")
			}
			o.Logger = o.Logger.Level(logLevel)
			return nil
		},
	}
	cmd.PersistentFlags().StringVar(&o.LogLevel, "log-level", log.LevelInfoValue, "Log level (trace, debug, info, warn, error, fatal, panic)")

	cmd.AddCommand(run.NewCommand(o))
	cmd.AddCommand(wait.NewCommand(o))
	cmd.AddCommand(status.NewCommand(o))
	cmd.AddCommand(stop.NewCommand(o))

	return cmd
}

func Execute() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	logger := log.New(
		log.ConsoleWriter{Out: os.Stderr},
	).With().Timestamp().Logger()

	go func() {
		<-ctx.Done()
		cancel()
	}()

	opts := options.NewOptions(
		options.WithContext(ctx),
		options.WithLogger(logger),
	)

	if err := NewCommand(opts).Execute(); err != nil {
		os.Exit(1)
	}
}
