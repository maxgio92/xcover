package healthcheck

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockConn implements the net.Conn interface for testing purposes
type MockConn struct {
	mock.Mock
}

// Implementing the net.Conn interface methods

func (m *MockConn) Read(b []byte) (n int, err error) {
	args := m.Called(b)
	return args.Int(0), args.Error(1)
}

func (m *MockConn) Write(b []byte) (n int, err error) {
	args := m.Called(b)
	return args.Int(0), args.Error(1)
}

func (m *MockConn) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockConn) LocalAddr() net.Addr {
	args := m.Called()
	return args.Get(0).(net.Addr)
}

func (m *MockConn) RemoteAddr() net.Addr {
	args := m.Called()
	return args.Get(0).(net.Addr)
}

func (m *MockConn) SetDeadline(t time.Time) error {
	args := m.Called(t)
	return args.Error(0)
}

func (m *MockConn) SetReadDeadline(t time.Time) error {
	args := m.Called(t)
	return args.Error(0)
}

func (m *MockConn) SetWriteDeadline(t time.Time) error {
	args := m.Called(t)
	return args.Error(0)
}

func TestHealthCheckServer_InitializeListener(t *testing.T) {
	t.Run("should start UDS listener without errors", func(t *testing.T) {
		logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
		hcs := NewHealthCheckServer("/tmp/server.sock", logger)

		os.Remove("/tmp/server.sock")
		ln, err := net.Listen("unix", "/tmp/server.sock")
		assert.Nil(t, err)
		hcs.ln = ln

		err = hcs.InitializeListener(context.Background())
		assert.Nil(t, err)
	})
}

func TestHealthCheckServer_NotifyReadiness(t *testing.T) {
	t.Run("should write readiness message when ready", func(t *testing.T) {
		logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
		hcs := NewHealthCheckServer("/tmp/server.sock", logger)

		// Trigger the readiness.
		hcs.NotifyReadiness()

		// Test that the readyCh channel is closed.
		assert.Panics(t, func() {
			hcs.readyCh <- struct{}{}
		})

		// Mock connection.
		mockConn := new(MockConn)

		// Verify the readiness message was sent.
		mockConn.On("Write", []byte{ReadyMsg}).Return(len([]byte{ReadyMsg}), nil)
		mockConn.On("Close").Return(nil)
		mockConn.On("SetReadDeadline", mock.Anything).Return(nil)
		//mockConn.On("Read", mock.AnythingOfType("[]uint8")).Return(0, io.EOF)
		mockConn.On("Read", mock.AnythingOfType("[]uint8")).Return(1, nil)

		hcs.processConnection(context.Background(), mockConn)

		mockConn.AssertExpectations(t)
	})
}

func TestHealthCheckServer_ShutdownListener(t *testing.T) {
	t.Run("should properly shut down listener and remove socket", func(t *testing.T) {
		logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
		hcs := NewHealthCheckServer("/tmp/server.sock", logger)

		// Mock net.Listener.
		os.Remove("/tmp/server.sock")
		ln, err := net.Listen("unix", "/tmp/server.sock")
		assert.Nil(t, err)
		hcs.ln = ln

		// Start listener in a goroutine.
		go hcs.acceptConnections(context.Background())

		// Stop listener.
		err = hcs.ShutdownListener()
		assert.Nil(t, err)

		// Verify the listener is closed properly.
		fi, err := os.Stat(hcs.socketPath)
		assert.Nil(t, fi)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})
}
