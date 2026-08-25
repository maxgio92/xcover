//go:build !userspace

package cmd

import (
	"github.com/maxgio92/xcover/pkg/cmd/options"
	"github.com/spf13/cobra"
)

// registerUserspaceCommands is a no-op in non-userspace builds.
func registerUserspaceCommands(_ *cobra.Command, _ *options.Options) {}
