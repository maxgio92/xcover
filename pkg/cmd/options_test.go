package cmd

import (
	"context"
	"testing"

	log "github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/cmd/options"
)

func TestNewOptions(t *testing.T) {
	tests := []struct {
		name     string
		options  []Option
		validate func(*testing.T, *Options)
	}{
		{
			name:    "empty options",
			options: []Option{},
			validate: func(t *testing.T, opts *Options) {
				require.NotNil(t, opts)
				require.NotNil(t, opts.CommonOptions)
				require.Empty(t, opts.Probe)
				require.Empty(t, opts.ProbeObjName)
			},
		},
		{
			name: "with probe",
			options: []Option{
				WithProbe([]byte("test probe")),
			},
			validate: func(t *testing.T, opts *Options) {
				require.Equal(t, []byte("test probe"), opts.Probe)
			},
		},
		{
			name: "with probe object name",
			options: []Option{
				WithProbeObjName("test.o"),
			},
			validate: func(t *testing.T, opts *Options) {
				require.Equal(t, "test.o", opts.ProbeObjName)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := NewOptions(tt.options...)
			require.NotNil(t, opts)

			if tt.validate != nil {
				tt.validate(t, opts)
			}
		})
	}
}

func TestWithProbe(t *testing.T) {
	tests := []struct {
		name     string
		probe    []byte
		validate func(*testing.T, *Options)
	}{
		{
			name:  "valid probe",
			probe: []byte("test probe data"),
			validate: func(t *testing.T, opts *Options) {
				require.Equal(t, []byte("test probe data"), opts.Probe)
			},
		},
		{
			name:  "empty probe",
			probe: []byte{},
			validate: func(t *testing.T, opts *Options) {
				require.Equal(t, []byte{}, opts.Probe)
			},
		},
		{
			name:  "nil probe",
			probe: nil,
			validate: func(t *testing.T, opts *Options) {
				require.Nil(t, opts.Probe)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &Options{}
			option := WithProbe(tt.probe)

			option(opts)

			if tt.validate != nil {
				tt.validate(t, opts)
			}
		})
	}
}

func TestWithProbeObjName(t *testing.T) {
	tests := []struct {
		name     string
		objName  string
		validate func(*testing.T, *Options)
	}{
		{
			name:    "valid object name",
			objName: "test.o",
			validate: func(t *testing.T, opts *Options) {
				require.Equal(t, "test.o", opts.ProbeObjName)
			},
		},
		{
			name:    "empty object name",
			objName: "",
			validate: func(t *testing.T, opts *Options) {
				require.Equal(t, "", opts.ProbeObjName)
			},
		},
		{
			name:    "object name with path",
			objName: "/path/to/object.o",
			validate: func(t *testing.T, opts *Options) {
				require.Equal(t, "/path/to/object.o", opts.ProbeObjName)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &Options{}
			option := WithProbeObjName(tt.objName)

			option(opts)

			if tt.validate != nil {
				tt.validate(t, opts)
			}
		})
	}
}

func TestWithContext(t *testing.T) {
	ctx := context.Background()
	opts := &Options{}
	opts.CommonOptions = &options.CommonOptions{}

	option := WithContext(ctx)
	option(opts)

	require.Equal(t, ctx, opts.Ctx)
}

func TestWithLogger(t *testing.T) {
	logger := log.New(log.ConsoleWriter{})
	opts := NewOptions()

	option := WithLogger(logger)
	option(opts)

	// Note: zerolog.Logger doesn't have direct equality comparison
	// so we test that it was set by using it
	require.NotPanics(t, func() {
		opts.Logger.Info().Msg("test")
	})
}

func TestOptionsChaining(t *testing.T) {
	ctx := context.Background()
	logger := log.New(log.ConsoleWriter{})
	probe := []byte("test probe")
	objName := "test.o"

	opts := NewOptions(
		WithContext(ctx),
		WithLogger(logger),
		WithProbe(probe),
		WithProbeObjName(objName),
	)

	require.Equal(t, ctx, opts.Ctx)
	require.Equal(t, probe, opts.Probe)
	require.Equal(t, objName, opts.ProbeObjName)
}

func TestOptionsOverride(t *testing.T) {
	opts := NewOptions()

	// Set initial probe
	WithProbe([]byte("first probe"))(opts)
	require.Equal(t, []byte("first probe"), opts.Probe)

	// Override with new probe
	WithProbe([]byte("second probe"))(opts)
	require.Equal(t, []byte("second probe"), opts.Probe)

	// Set initial object name
	WithProbeObjName("first.o")(opts)
	require.Equal(t, "first.o", opts.ProbeObjName)

	// Override with new object name
	WithProbeObjName("second.o")(opts)
	require.Equal(t, "second.o", opts.ProbeObjName)
}

func TestOptionsWithNilPointer(t *testing.T) {
	// Test that options handle nil gracefully
	require.NotPanics(t, func() {
		WithProbe([]byte("test"))(nil)
		WithProbeObjName("test.o")(nil)
		WithContext(context.Background())(nil)
		WithLogger(log.New(log.ConsoleWriter{}))(nil)
	})
}

func TestOptionsStruct(t *testing.T) {
	opts := &Options{}

	// Test default values
	require.Nil(t, opts.Probe)
	require.Equal(t, "", opts.ProbeObjName)
	require.Nil(t, opts.CommonOptions)
}

func TestOptionsIntegration(t *testing.T) {
	// Test realistic option combinations
	ctx := context.Background()
	logger := log.New(log.ConsoleWriter{})
	probe := []byte("realistic probe data with some content")
	objName := "realistic_object.o"

	opts := NewOptions(
		WithContext(ctx),
		WithLogger(logger),
		WithProbe(probe),
		WithProbeObjName(objName),
	)

	// Verify all options were applied correctly
	require.Equal(t, ctx, opts.Ctx)
	require.Equal(t, probe, opts.Probe)
	require.Equal(t, objName, opts.ProbeObjName)
	require.NotNil(t, opts.CommonOptions)

	// Test that the options work with command creation
	cmd := NewCommand(opts)
	require.NotNil(t, cmd)
	require.Equal(t, "xcover", cmd.Name())
}

func TestOptionsMemoryUsage(t *testing.T) {
	// Test that creating many options doesn't cause memory issues
	const numOptions = 100

	for i := 0; i < numOptions; i++ {
		opts := NewOptions(
			WithProbe([]byte("test probe")),
			WithProbeObjName("test.o"),
			WithContext(context.Background()),
			WithLogger(log.New(log.ConsoleWriter{})),
		)
		require.NotNil(t, opts)
	}
}

func TestOptionsConcurrency(t *testing.T) {
	// Test concurrent option creation
	const numGoroutines = 10
	done := make(chan *Options, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			ctx := context.Background()
			logger := log.New(log.ConsoleWriter{})
			opts := NewOptions(
				WithContext(ctx),
				WithLogger(logger),
				WithProbe([]byte("concurrent test")),
				WithProbeObjName("concurrent.o"),
			)
			done <- opts
		}(i)
	}

	// Collect results
	results := make([]*Options, 0, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		result := <-done
		results = append(results, result)
	}

	// Verify all results
	require.Len(t, results, numGoroutines)
	for i, result := range results {
		require.NotNil(t, result, "Result %d should not be nil", i)
		require.Equal(t, []byte("concurrent test"), result.Probe)
		require.Equal(t, "concurrent.o", result.ProbeObjName)
	}
}

// Benchmark tests
func BenchmarkNewOptions(b *testing.B) {
	ctx := context.Background()
	logger := log.New(log.ConsoleWriter{})
	probe := []byte("benchmark probe")
	objName := "benchmark.o"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewOptions(
			WithContext(ctx),
			WithLogger(logger),
			WithProbe(probe),
			WithProbeObjName(objName),
		)
	}
}

func BenchmarkWithProbe(b *testing.B) {
	opts := &Options{}
	probe := []byte("benchmark probe data")
	option := WithProbe(probe)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		option(opts)
	}
}

func BenchmarkOptionsChaining(b *testing.B) {
	ctx := context.Background()
	logger := log.New(log.ConsoleWriter{})
	probe := []byte("benchmark probe")
	objName := "benchmark.o"

	options := []Option{
		WithContext(ctx),
		WithLogger(logger),
		WithProbe(probe),
		WithProbeObjName(objName),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opts := &Options{}
		for _, option := range options {
			option(opts)
		}
	}
}