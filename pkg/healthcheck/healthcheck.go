package healthcheck

import (
	"context"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/pkg/errors"

	log "github.com/rs/zerolog"
)

const ReadyMsg = 0x01

type HealthCheckServer struct {
	ln net.Listener
	// acceptDone is closed when acceptConnections returns, so that
	// ShutdownListener can guarantee the accept loop has stopped.
	acceptDone chan struct{}
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
// A socket path that a running listener still answers on is left in place
// and reported as an error, so the listener is never bound.
func (s *HealthCheckServer) InitializeListener(ctx context.Context) error {
	if err := s.removeStaleSocket(ctx); err != nil {
		return err
	}

	// Create UDS listener.
	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		// Returned, not logged: the caller wraps and reports it once.
		return errors.Wrap(err, "failed to listen on UDS")
	}
	s.ln = ln
	s.acceptDone = make(chan struct{})

	// Start accepting connections.
	go func() {
		defer close(s.acceptDone)
		s.acceptConnections(ctx)
	}()

	return nil
}

// removeStaleSocket unlinks the socket path only when nothing answers on it.
// A regular file, a socket with no listener or a dangling symlink refuses the
// probe and is removed. A live listener accepts it and is reported as an
// error naming the path. Any other probe failure, such as EACCES or a timeout
// on a wedged listener, is reported without removing anything.
func (s *HealthCheckServer) removeStaleSocket(ctx context.Context) error {
	// Lstat: a dangling symlink still occupies the path and would fail the
	// bind, so it must be probed and removed, not reported absent.
	if _, err := os.Lstat(s.socketPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.Wrapf(err, "failed to stat %s", s.socketPath)
	}

	// The timeout keeps a listener that never accepts from stalling startup.
	dialer := net.Dialer{Timeout: time.Second}
	conn, err := dialer.DialContext(ctx, "unix", s.socketPath)
	if err == nil {
		conn.Close()
		return errors.Errorf("%s is in use by a running listener", s.socketPath)
	}
	if !errors.Is(err, syscall.ECONNREFUSED) && !errors.Is(err, os.ErrNotExist) {
		return errors.Wrapf(err, "failed to probe %s", s.socketPath)
	}

	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "failed to remove stale socket %s", s.socketPath)
	}
	return nil
}

// NotifyReadiness should be called by the UserTracer or when the tool is ready.
func (s *HealthCheckServer) NotifyReadiness() {
	s.logger.Debug().Msg("marking readiness")
	close(s.readyCh)
}

// ShutdownListener gracefully shuts down the listener, waits for the accept
// loop to stop and removes the socket.
func (s *HealthCheckServer) ShutdownListener() error {
	// Ensure the listener is closed properly.
	if s.ln != nil {
		if err := s.ln.Close(); err != nil {
			s.logger.Debug().Err(err).Msg("error closing listener")
		}
		<-s.acceptDone
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
