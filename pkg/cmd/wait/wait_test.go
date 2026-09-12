package wait

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/options"
)

func withTempPidFile(t *testing.T) string {
	t.Helper()

	orig := settings.PidFile
	settings.PidFile = filepath.Join(t.TempDir(), "test.pid")
	t.Cleanup(func() { settings.PidFile = orig })

	return settings.PidFile
}

func TestRun_NotRunning(t *testing.T) {
	path := withTempPidFile(t)
	// A PID that cannot belong to a live process.
	require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(1<<22+1)), 0644))

	o := &Options{
		Options:    options.NewOptions(),
		socketPath: filepath.Join(t.TempDir(), "missing.sock"),
		timeout:    10 * time.Second,
	}

	require.ErrorIs(t, o.Run(nil, nil), ErrNotRunning)
}

// TestRun_DaemonExitsWhilePolling asserts the polling loop fails fast once
// the daemon dies, instead of spinning until --timeout.
func TestRun_DaemonExitsWhilePolling(t *testing.T) {
	path := withTempPidFile(t)

	child := exec.Command("sleep", "60")
	require.NoError(t, child.Start())
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(child.Process.Pid)), 0644))

	// Listen on the socket without ever sending ReadyMsg: Run connecting to
	// it is the observable proof that the initial liveness check has passed
	// and the loop is polling, so the child can be killed without a timing
	// guess. Run then sleeps one retry interval, re-checks liveness and must
	// return ErrExited.
	sockPath := filepath.Join(t.TempDir(), "hc.sock")
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	o := &Options{
		Options:    options.NewOptions(),
		socketPath: sockPath,
		timeout:    30 * time.Second,
	}

	done := make(chan error, 1)
	go func() { done <- o.Run(nil, nil) }()

	// Bound the accept so a Run that returns before dialing fails the test
	// with its error instead of hanging until the package timeout.
	require.NoError(t, ln.(*net.UnixListener).SetDeadline(time.Now().Add(5*time.Second)))
	conn, err := ln.Accept()
	require.NoError(t, err)
	// Kill and reap the child so Signal(0) stops succeeding, then hang up so
	// Run's read fails and it goes round the loop again.
	require.NoError(t, child.Process.Kill())
	_ = child.Wait()
	require.NoError(t, conn.Close())

	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrExited)
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not fail fast after the daemon exited")
	}
}
