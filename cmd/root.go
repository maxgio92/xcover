package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	log "github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/maxgio92/utrace/cmd/profile"
	"github.com/maxgio92/utrace/internal/commands/options"
)

func NewRootCmd(opts *options.CommonOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "yap",
		Short:             "yap is Yet Another Profiler",
		Long:              `YAP is a kernel-assisted low-overhead sampling-based CPU profiler.`,
		DisableAutoGenTag: true,
	}
	cmd.AddCommand(profile.NewCommand(opts))
	cmd.PersistentFlags().BoolVar(&opts.Debug, "debug", false, "Sets log level to debug")

	return cmd
}

// Execute adds all child commands to the root commands and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(probe []byte) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	logger := log.New(os.Stderr).Level(log.InfoLevel)

	go func() {
		<-ctx.Done()
		logger.Info().Msg("terminating...")
		cancel()
	}()

	opts := options.NewCommonOptions(
		options.WithProbe(probe),
		options.WithContext(ctx),
		options.WithLogger(logger),
	)

	if err := NewRootCmd(opts).Execute(); err != nil {
		os.Exit(1)
	}
}
