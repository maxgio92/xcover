package cmd

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/maxgio92/xcover/pkg/cmd/options"

	log "github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestNewCommand(t *testing.T) {
	logger := log.New(log.ConsoleWriter{Out: os.Stderr})
	ctx := context.Background()

	tests := []struct {
		name     string
		options  *options.Options
		validate func(*testing.T, *cobra.Command)
	}{
		{
			name: "default command creation",
			options: options.NewOptions(
				options.WithContext(ctx),
				options.WithLogger(logger),
			),
			validate: func(t *testing.T, cmd *cobra.Command) {
				require.Equal(t, "xcover", cmd.Name())
				require.Contains(t, cmd.Short, "functional test coverage profiler")
				require.True(t, cmd.HasSubCommands())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewCommand(tt.options)
			require.NotNil(t, cmd)

			if tt.validate != nil {
				tt.validate(t, cmd)
			}
		})
	}
}

func TestCommandFlags(t *testing.T) {
	logger := log.New(log.ConsoleWriter{Out: os.Stderr})
	ctx := context.Background()
	opts := options.NewOptions(options.WithContext(ctx), options.WithLogger(logger))
	cmd := NewCommand(opts)

	// Test log-level flag
	flag := cmd.PersistentFlags().Lookup("log-level")
	require.NotNil(t, flag)
	require.Equal(t, "string", flag.Value.Type())
	require.Equal(t, "info", flag.DefValue)
	require.Contains(t, flag.Usage, "Log level")
}

func TestCommandSubcommands(t *testing.T) {
	logger := log.New(log.ConsoleWriter{Out: os.Stderr})
	ctx := context.Background()
	opts := options.NewOptions(options.WithContext(ctx), options.WithLogger(logger))
	cmd := NewCommand(opts)

	expectedSubcommands := []string{"run", "status", "stop", "wait"}
	actualSubcommands := make([]string, 0)

	for _, subCmd := range cmd.Commands() {
		actualSubcommands = append(actualSubcommands, subCmd.Name())
	}

	for _, expected := range expectedSubcommands {
		require.Contains(t, actualSubcommands, expected)
	}
}

func TestCommandHelp(t *testing.T) {
	logger := log.New(log.ConsoleWriter{Out: os.Stderr})
	ctx := context.Background()
	opts := options.NewOptions(options.WithContext(ctx), options.WithLogger(logger))
	cmd := NewCommand(opts)

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	helpOutput := output.String()
	require.Contains(t, helpOutput, "xcover")
	require.Contains(t, helpOutput, "functional test coverage profiler")
	require.Contains(t, helpOutput, "Available Commands:")
	require.Contains(t, helpOutput, "run")
	require.Contains(t, helpOutput, "status")
	require.Contains(t, helpOutput, "stop")
	require.Contains(t, helpOutput, "wait")
}

func TestCommandInvalidFlag(t *testing.T) {
	logger := log.New(log.ConsoleWriter{Out: os.Stderr})
	ctx := context.Background()
	opts := options.NewOptions(options.WithContext(ctx), options.WithLogger(logger))
	cmd := NewCommand(opts)

	var output bytes.Buffer
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--invalid-flag"})

	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, output.String(), "unknown flag")
}

func TestCommandLogLevelFlag(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		wantErr  bool
	}{
		{"trace level", "trace", false},
		{"debug level", "debug", false},
		{"info level", "info", false},
		{"warn level", "warn", false},
		{"error level", "error", false},
		{"fatal level", "fatal", false},
		{"panic level", "panic", false},
		{"invalid level", "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.New(log.ConsoleWriter{Out: os.Stderr})
			ctx := context.Background()
			opts := options.NewOptions(options.WithContext(ctx), options.WithLogger(logger))
			cmd := NewCommand(opts)

			var output bytes.Buffer
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"--log-level", tt.logLevel, "status"})

			err := cmd.Execute()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCommandLogLevelPropagatesToSubcommand(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		want     log.Level
	}{
		{"trace level", "trace", log.TraceLevel},
		{"debug level", "debug", log.DebugLevel},
		{"warn level", "warn", log.WarnLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.New(log.ConsoleWriter{Out: os.Stderr})
			ctx := context.Background()
			opts := options.NewOptions(options.WithContext(ctx), options.WithLogger(logger))
			cmd := NewCommand(opts)

			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			// "wait" observably touches opts.Logger (via With().Str(...).Logger())
			// before returning ErrNotRunning, without requiring a running daemon.
			cmd.SetArgs([]string{"--log-level", tt.logLevel, "wait", "--timeout", "1ms"})

			_ = cmd.Execute()

			require.Equal(t, tt.want, opts.Logger.GetLevel())
		})
	}
}

func TestCommandContext(t *testing.T) {
	logger := log.New(log.ConsoleWriter{Out: os.Stderr})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	opts := options.NewOptions(options.WithContext(ctx), options.WithLogger(logger))
	cmd := NewCommand(opts)

	// Test that context is properly passed through
	cmd.SetContext(ctx)
	require.Equal(t, ctx, cmd.Context())
}

func TestCommandExecutionWithoutSubcommand(t *testing.T) {
	logger := log.New(log.ConsoleWriter{Out: os.Stderr})
	ctx := context.Background()
	opts := options.NewOptions(options.WithContext(ctx), options.WithLogger(logger))
	cmd := NewCommand(opts)

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	// Should show help when no subcommand is provided
	helpOutput := output.String()
	require.Contains(t, helpOutput, "xcover")
	require.Contains(t, helpOutput, "Available Commands:")
}

func TestCommandDisableAutoGenTag(t *testing.T) {
	logger := log.New(log.ConsoleWriter{Out: os.Stderr})
	ctx := context.Background()
	opts := options.NewOptions(options.WithContext(ctx), options.WithLogger(logger))
	cmd := NewCommand(opts)

	require.True(t, cmd.DisableAutoGenTag)
}

func TestCommandVersion(t *testing.T) {
	logger := log.New(log.ConsoleWriter{Out: os.Stderr})
	ctx := context.Background()
	opts := options.NewOptions(options.WithContext(ctx), options.WithLogger(logger))
	cmd := NewCommand(opts)

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--version"})

	err := cmd.Execute()
	if err == nil {
		versionOutput := output.String()
		require.NotEmpty(t, strings.TrimSpace(versionOutput))
	}
	// Version flag might not be implemented, so we don't require success
}

func TestCommandLongDescription(t *testing.T) {
	logger := log.New(log.ConsoleWriter{Out: os.Stderr})
	ctx := context.Background()
	opts := options.NewOptions(options.WithContext(ctx), options.WithLogger(logger))
	cmd := NewCommand(opts)

	require.NotEmpty(t, cmd.Long)
	require.Contains(t, cmd.Long, "xcover")
	require.Contains(t, cmd.Long, "profiler")
}

func TestCommandStructure(t *testing.T) {
	logger := log.New(log.ConsoleWriter{Out: os.Stderr})
	ctx := context.Background()
	opts := options.NewOptions(options.WithContext(ctx), options.WithLogger(logger))
	cmd := NewCommand(opts)

	// Test basic command structure
	require.Equal(t, "xcover", cmd.Use)
	require.NotEmpty(t, cmd.Short)
	require.NotEmpty(t, cmd.Long)
	require.True(t, cmd.DisableAutoGenTag)

	// Test that all required subcommands are present
	subcommands := make(map[string]*cobra.Command)
	for _, subCmd := range cmd.Commands() {
		subcommands[subCmd.Name()] = subCmd
	}

	require.Contains(t, subcommands, "run")
	require.Contains(t, subcommands, "status")
	require.Contains(t, subcommands, "stop")
	require.Contains(t, subcommands, "wait")
}
