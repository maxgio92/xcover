package wait

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/common"
	"github.com/maxgio92/xcover/pkg/cmd/options"
)

func withTempPidFile(t *testing.T) string {
	t.Helper()

	orig := settings.PidFile
	settings.PidFile = filepath.Join(t.TempDir(), "test.pid")
	t.Cleanup(func() { settings.PidFile = orig })

	return settings.PidFile
}

func withTempLogFile(t *testing.T) string {
	t.Helper()

	orig := settings.LogFile
	settings.LogFile = filepath.Join(t.TempDir(), "xcover.log")
	t.Cleanup(func() { settings.LogFile = orig })

	return settings.LogFile
}

// seedLog writes two lines to a temp daemon log, the second wrapped in an
// SGR sequence, and returns exactly what PrintLogTail must write for it.
func seedLog(t *testing.T) string {
	t.Helper()

	path := withTempLogFile(t)
	require.NoError(t, os.WriteFile(path, []byte("failed to load bpf module trace\n\x1b[31mboom\x1b[0m\n"), 0644))

	return "tail of " + path + " (2 lines):\nfailed to load bpf module trace\nboom\n"
}

func TestRun_NotRunning(t *testing.T) {
	path := withTempPidFile(t)
	// A PID that cannot belong to a live process.
	require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(1<<22+1)), 0644))
	want := seedLog(t)

	var buf bytes.Buffer
	o := &Options{
		Options:    options.NewOptions(),
		socketPath: filepath.Join(t.TempDir(), "missing.sock"),
		timeout:    10 * time.Second,
		errOut:     &buf,
	}

	err := o.Run(nil, nil)
	require.ErrorIs(t, err, ErrNotRunning)
	require.ErrorIs(t, err, common.ErrNotRunning)
	require.EqualError(t, err, "xcover is not running: stale PID file "+path+" (PID 4194305)")
	require.Equal(t, want, buf.String())
}

func TestRun_MissingPIDFile(t *testing.T) {
	path := withTempPidFile(t)
	want := seedLog(t)

	var buf bytes.Buffer
	o := &Options{
		Options:    options.NewOptions(),
		socketPath: filepath.Join(t.TempDir(), "missing.sock"),
		timeout:    10 * time.Second,
		errOut:     &buf,
	}

	err := o.Run(nil, nil)
	require.ErrorIs(t, err, common.ErrNotRunning)
	require.EqualError(t, err, "xcover is not running: PID file "+path+" not found")
	require.Equal(t, want, buf.String())
}

func TestRun_MalformedPIDFile(t *testing.T) {
	path := withTempPidFile(t)
	require.NoError(t, os.WriteFile(path, []byte("not-a-pid"), 0644))
	want := seedLog(t)

	var buf bytes.Buffer
	o := &Options{
		Options:    options.NewOptions(),
		socketPath: filepath.Join(t.TempDir(), "missing.sock"),
		timeout:    10 * time.Second,
		errOut:     &buf,
	}

	err := o.Run(nil, nil)
	require.ErrorIs(t, err, common.ErrInvalidPIDFile)
	require.EqualError(t, err, "invalid PID file "+path)
	require.Equal(t, want, buf.String())
}

// TestRun_DaemonExitsWhilePolling asserts the polling loop fails fast once
// the daemon dies, instead of spinning until --timeout.
func TestRun_DaemonExitsWhilePolling(t *testing.T) {
	path := withTempPidFile(t)
	want := seedLog(t)

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

	var buf bytes.Buffer
	o := &Options{
		Options:    options.NewOptions(),
		socketPath: sockPath,
		timeout:    30 * time.Second,
		errOut:     &buf,
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
		require.Equal(t, want, buf.String())
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not fail fast after the daemon exited")
	}
}

// TestRun_Timeout asserts a live daemon whose socket never appears ends in
// ErrTimeout, with the log tail written before the return.
func TestRun_Timeout(t *testing.T) {
	path := withTempPidFile(t)
	want := seedLog(t)

	child := exec.Command("sleep", "60")
	require.NoError(t, child.Start())
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(child.Process.Pid)), 0644))

	var buf bytes.Buffer
	o := &Options{
		Options:    options.NewOptions(),
		socketPath: filepath.Join(t.TempDir(), "missing.sock"),
		timeout:    0,
		errOut:     &buf,
	}

	err := o.Run(nil, nil)
	require.ErrorIs(t, err, ErrTimeout)
	require.EqualError(t, err, "timeout waiting for profiler readiness")
	require.Equal(t, want, buf.String())
}
