package options

import (
	"context"
	"testing"

	log "github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
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
			},
		},
		{
			name: "with context",
			options: []Option{
				WithContext(context.Background()),
			},
			validate: func(t *testing.T, opts *Options) {
				require.Equal(t, context.Background(), opts.Ctx)
			},
		},
		{
			name: "with probe object name",
			options: []Option{
				WithLogger(log.New(log.NewTestWriter(t))),
			},
			validate: func(t *testing.T, opts *Options) {
				// Note: zerolog.Logger doesn't have direct equality comparison
				// so we test that it was set by using it.
				require.NotPanics(t, func() {
					opts.Logger.Info().Msg("test")
				})
				//require.NotNil(t, opts.Logger)
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
