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
	"github.com/maxgio92/xcover/pkg/cmd/profile"
	"github.com/maxgio92/xcover/pkg/cmd/wait"
)

func NewCommand(o *Options) *cobra.Command {
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
			settings.CmdName, profile.CmdName, wait.CmdName),
		DisableAutoGenTag: true,
	}
	cmd.PersistentFlags().StringVar(&o.LogLevel, "log-level", log.LevelInfoValue, "Log level (trace, debug, info, warn, error, fatal, panic)")

	cmd.AddCommand(profile.NewCommand(
		profile.NewOptions(
			profile.WithProbe(o.Probe),
			profile.WithProbeObjName(o.ProbeObjName),
			profile.WithContext(o.Ctx),
			profile.WithLogger(o.Logger),
			profile.WithLogLevel(o.LogLevel),
		),
	))
	cmd.AddCommand(wait.NewCommand(
		wait.NewOptions(
			wait.WithContext(o.Ctx),
			wait.WithLogger(o.Logger),
			wait.WithLogLevel(o.LogLevel),
		),
	))

	return cmd
}

func Execute(probe []byte, probeObjName string) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	logger := log.New(
		log.ConsoleWriter{Out: os.Stderr},
	).With().Timestamp().Logger()

	go func() {
		<-ctx.Done()
		cancel()
	}()

	opts := NewOptions(
		WithProbe(probe),
		WithProbeObjName(probeObjName),
		WithContext(ctx),
		WithLogger(logger),
	)

	if err := NewCommand(opts).Execute(); err != nil {
		os.Exit(1)
	}
}
