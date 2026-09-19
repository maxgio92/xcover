package healthcheck

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer starts a health check server on a socket private to the
// test and stops it, including its accept loop, when the test ends.
func newTestServer(t *testing.T) *HealthCheckServer {
	t.Helper()

	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	hcs := NewHealthCheckServer(filepath.Join(t.TempDir(), "hc.sock"), logger)

	require.NoError(t, hcs.InitializeListener(t.Context()))
	t.Cleanup(func() { _ = hcs.ShutdownListener() })

	return hcs
}

func TestHealthCheckServer_InitializeListener(t *testing.T) {
	t.Run("should start UDS listener without errors", func(t *testing.T) {
		hcs := newTestServer(t)

		fi, err := os.Stat(hcs.socketPath)
		require.NoError(t, err)
		assert.NotZero(t, fi.Mode()&os.ModeSocket)
	})

	t.Run("should replace a stale socket file", func(t *testing.T) {
		socketPath := filepath.Join(t.TempDir(), "hc.sock")
		require.NoError(t, os.WriteFile(socketPath, nil, 0o600))

		hcs := NewHealthCheckServer(socketPath, zerolog.Nop())
		require.NoError(t, hcs.InitializeListener(t.Context()))
		t.Cleanup(func() { _ = hcs.ShutdownListener() })

		fi, err := os.Stat(socketPath)
		require.NoError(t, err)
		assert.NotZero(t, fi.Mode()&os.ModeSocket)
	})
}

func TestHealthCheckServer_NotifyReadiness(t *testing.T) {
	t.Run("should write readiness message to clients once ready", func(t *testing.T) {
		hcs := newTestServer(t)

		// Connect before readiness: the server must hold the connection
		// and answer only after NotifyReadiness.
		conn, err := net.DialTimeout("unix", hcs.socketPath, time.Second)
		require.NoError(t, err)
		t.Cleanup(func() { conn.Close() })

		hcs.NotifyReadiness()

		buf := make([]byte, 1)
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		n, err := conn.Read(buf)
		require.NoError(t, err)
		require.Equal(t, 1, n)
		assert.Equal(t, byte(ReadyMsg), buf[0])
	})

	t.Run("should not write when context is cancelled before readiness", func(t *testing.T) {
		hcs := NewHealthCheckServer(filepath.Join(t.TempDir(), "hc.sock"), zerolog.Nop())

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		server, client := net.Pipe()
		t.Cleanup(func() { client.Close() })

		hcs.processConnection(ctx, server)

		// processConnection closes its end on return; the client observes
		// EOF without ever receiving a readiness byte.
		buf := make([]byte, 1)
		n, err := client.Read(buf)
		assert.Zero(t, n)
		assert.Error(t, err)
	})
}

func TestHealthCheckServer_ShutdownListener(t *testing.T) {
	t.Run("should properly shut down listener and remove socket", func(t *testing.T) {
		hcs := newTestServer(t)

		require.NoError(t, hcs.ShutdownListener())

		fi, err := os.Stat(hcs.socketPath)
		assert.Nil(t, fi)
		assert.ErrorIs(t, err, os.ErrNotExist)

		// The listener is closed: new clients are refused.
		_, err = net.DialTimeout("unix", hcs.socketPath, time.Second)
		assert.Error(t, err)
	})

	t.Run("should be safe to call without a listener", func(t *testing.T) {
		hcs := NewHealthCheckServer(filepath.Join(t.TempDir(), "hc.sock"), zerolog.Nop())
		assert.NoError(t, hcs.ShutdownListener())
	})
}
