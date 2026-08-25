//go:build userspace

package cmd

import (
	"github.com/maxgio92/xcover/pkg/cmd/agent"
	"github.com/maxgio92/xcover/pkg/cmd/options"
	"github.com/spf13/cobra"
)

// registerUserspaceCommands adds subcommands that are only available in
// userspace-BPF builds (i.e. built with -tags userspace).
func registerUserspaceCommands(cmd *cobra.Command, o *options.Options) {
	cmd.AddCommand(agent.NewCommand(o))
}
