package healthcheck

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/pkg/errors"

	log "github.com/rs/zerolog"
)

const ReadyMsg = 0x01

type HealthCheckServer struct {
	ln         net.Listener
	readyCh    chan struct{}
	socketPath string
	logger     log.Logger
}

// NewHealthCheckServer creates a new health check server.
func NewHealthCheckServer(socketPath string, logger log.Logger) *HealthCheckServer {
	l := logger.With().Str("component", "healthcheck").Logger()
	return &HealthCheckServer{
		socketPath: socketPath,
		readyCh:    make(chan struct{}),
		logger:     l,
	}
}

// InitializeListener starts the UDS listener for accepting connections.
func (s *HealthCheckServer) InitializeListener(ctx context.Context) error {
	// Remove socket if it already exists.
	os.Remove(s.socketPath)

	// Create UDS listener.
	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		fmt.Println("failed to listen on UDS:")
		return errors.Wrap(err, "failed to listen on UDS")
	}
	s.ln = ln

	// Start accepting connections.
	go s.acceptConnections(ctx)

	return nil
}

// NotifyReadiness should be called by the UserTracer or when the tool is ready.
func (s *HealthCheckServer) NotifyReadiness() {
	s.logger.Debug().Msg("marking readiness")
	close(s.readyCh)
}

// ShutdownListener gracefully shuts down the listener and removes the socket.
func (s *HealthCheckServer) ShutdownListener() error {
	// Ensure the listener is closed properly.
	if s.ln != nil {
		if err := s.ln.Close(); err != nil {
			s.logger.Debug().Err(err).Msg("error closing listener")
		}
	}

	// Remove the socket file if it exists.
	if err := os.Remove(s.socketPath); err != nil {
		if !os.IsNotExist(err) {
			s.logger.Debug().Err(err).Msgf("error removing socket")
			return err
		}
		s.logger.Debug().Msg("ignoring removing socket file, as it is already removed")
	}

	return nil
}

// acceptConnections listens for incoming connections and handles them.
func (s *HealthCheckServer) acceptConnections(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			s.logger.Debug().Msg("stopping accepting connections")
			return // Shutdown gracefully.
		default:
			// Accept connections.
			conn, err := s.ln.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					s.logger.Debug().Msg("ignoring accepting connection as it is closed")
					return
				}
				s.logger.Warn().Err(err).Msg("accept error")
				continue
			}

			// Handle each connection.
			go s.processConnection(ctx, conn)
		}
	}
}

// processConnection handles each accepted connection and responds when ready.
func (s *HealthCheckServer) processConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	select {
	// Tracer is ready, send ready message.
	case <-s.readyCh:
		// Test that the connection is still open.
		if !s.isConnectionAlive(conn) {
			s.logger.Debug().Msg("connection is closed")
			return
		}
		if err := s.safeWrite(conn, []byte{ReadyMsg}); err != nil {
			if !errors.Is(err, syscall.EPIPE) && !errors.Is(err, syscall.ECONNRESET) {
				s.logger.Debug().Err(err).Msg("failed to write")
			}
		}
	case <-ctx.Done():
		// Graceful shutdown handling.
		s.logger.Debug().Msg("ignoring sending readiness message as context is canceled")
		return
	}
}

func (s *HealthCheckServer) isConnectionAlive(conn net.Conn) bool {
	// Decrease timeout to read fast.
	conn.SetReadDeadline(time.Now())
	if _, err := conn.Read([]byte{}); err == io.EOF {
		s.logger.Debug().Err(err).Msg("cannot write ready message: connection is already closed")
		conn.Close()

		return false
	}

	conn.SetReadDeadline(time.Time{})
	return true
}

func (s *HealthCheckServer) safeWrite(conn net.Conn, data []byte) error {
	_, err := conn.Write(data)
	if err != nil {
		switch {
		case errors.Is(err, syscall.EPIPE):
			conn.Close()
			return errors.Wrap(err, "peer closed the connection")
		case errors.Is(err, syscall.ECONNRESET):
			conn.Close()
			return errors.Wrap(err, "peer reset the connection")
		default:
			return errors.Wrap(err, "failed to write")
		}
	}
	return nil
}
