package stop

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/common"
)

func NewCommand(o *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "stop",
		Short:             fmt.Sprintf("Stop the %s profiler daemon", settings.CmdName),
		DisableAutoGenTag: true,
		SilenceUsage:      true,
		Run:               o.Run,
	}

	return cmd
}

func (o *Options) Run(cmd *cobra.Command, _ []string) {
	pidData, err := os.ReadFile(settings.PidFile)
	if err != nil {
		fmt.Printf("%s not running or PID file not found\n", settings.CmdName)
		return
	}

	pid, err := strconv.Atoi(string(pidData))
	if err != nil {
		fmt.Println("Invalid PID file")
		return
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		fmt.Println("Process not found")
		return
	}

	err = process.Signal(syscall.SIGTERM)
	if err != nil {
		fmt.Printf("Failed to stop daemon: %v\n", err)
		return
	}

	// Wait for process to stop.
	for i := 0; i < 50; i++ {
		if !common.IsDaemonRunning() {
			fmt.Printf("%s stopped (PID %d)\n", settings.CmdName, pid)
			os.Remove(settings.PidFile)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Force kill if still running.
	process.Kill()
	os.Remove(settings.PidFile)
	fmt.Printf("%s force killed (PID %d)\n", settings.CmdName, pid)
}
