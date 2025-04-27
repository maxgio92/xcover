package healthcheck

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"net"
	"os"

	log "github.com/rs/zerolog"
)

const ReadyMsg = 0x01

type HealthCheckServer struct {
	ln         net.Listener
	readyCh    chan struct{}
	socketPath string
	logger     *log.Logger
}

// NewHealthCheckServer creates a new health check server.
func NewHealthCheckServer(socketPath string, logger *log.Logger) *HealthCheckServer {
	l := logger.With().Str("component", "healthcheck").Logger()
	return &HealthCheckServer{
		socketPath: socketPath,
		readyCh:    make(chan struct{}),
		logger:     &l,
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
		s.logger.Debug().Err(err).Msgf("error removing socket")
	}

	return nil
}

// acceptConnections listens for incoming connections and handles them.
func (s *HealthCheckServer) acceptConnections(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			s.logger.Debug().Msg("stopping connection acceptance")
			return // Shutdown gracefully.
		default:
			// Accept connections.
			conn, err := s.ln.Accept()
			if err != nil {
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
	case <-s.readyCh:
		// Tracer is ready, send ready message.
		_, err := conn.Write([]byte{ReadyMsg})
		if err != nil {
			s.logger.Debug().Err(err).Msg("write error")
		}
	case <-ctx.Done():
		// Graceful shutdown handling.
		s.logger.Debug().Msg("context canceled, not sending readiness message")
		return
	}
}
