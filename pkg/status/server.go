package status

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/pprof"
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
	"go.uber.org/zap"
)

type StatusServer interface {
	Start() error
	Stop() error
	IsReady() bool
	Errors() <-chan error
}

type BaseStatusServer struct {
	listen     string
	logger     *zap.Logger
	readyCheck func() bool
	mux        *http.ServeMux

	mu      sync.Mutex
	server  *http.Server
	stopped bool
	errors  chan error

	ctx    context.Context
	cancel context.CancelCauseFunc
}

func NewBaseStatusServer(listen string, logger *zap.Logger, readyCheck func() bool) *BaseStatusServer {
	ctx, cancel := context.WithCancelCause(context.Background())
	s := &BaseStatusServer{
		listen:     listen,
		logger:     logger,
		readyCheck: readyCheck,
		mux:        http.NewServeMux(),
		errors:     make(chan error, 1),
		ctx:        ctx,
		cancel:     cancel,
	}

	s.setupRoutes()
	return s
}

func (s *BaseStatusServer) Mux() *http.ServeMux {
	return s.mux
}

func (s *BaseStatusServer) Start() error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return http.ErrServerClosed
	}
	if s.server != nil {
		s.mu.Unlock()
		return errors.New("status server already started")
	}
	s.mu.Unlock()

	server := &http.Server{
		// set a base context for graceful cancellation of in-flight requests
		// contexts passed to handlers will be derived from this base context
		BaseContext: func(l net.Listener) context.Context {
			return s.ctx
		},
		Addr:    s.listen,
		Handler: safeHandler(s.logger, s.mux),
	}
	listener, err := net.Listen("tcp", s.listen)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		_ = listener.Close()
		return http.ErrServerClosed
	}
	if s.server != nil {
		s.mu.Unlock()
		_ = listener.Close()
		return errors.New("status server already started")
	}
	s.server = server
	s.mu.Unlock()

	go s.serve(server, listener)

	s.logger.Info("status server listening", zap.String("url", "http://"+s.listen))
	return nil
}

func (s *BaseStatusServer) Errors() <-chan error {
	return s.errors
}

func (s *BaseStatusServer) serve(server *http.Server, listener net.Listener) {
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger.Error("status server stopped unexpectedly", zap.Error(err))
		select {
		case s.errors <- err:
		default:
		}
	}
}

func (s *BaseStatusServer) Stop() error {
	s.mu.Lock()
	server := s.server
	if server == nil {
		s.mu.Unlock()
		return nil
	}
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	s.mu.Unlock()

	// cancel the base context to gracefully cancel in-flight requests
	s.cancel(errors.New("server shutdown"))

	// shutdown the server with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

func (s *BaseStatusServer) IsReady() bool {
	return s.readyCheck()
}

func (s *BaseStatusServer) setupRoutes() {
	s.mux.HandleFunc("/debug/pprof/", pprof.Index)
	s.mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	s.mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	s.mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	s.mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	lifecycleHandler := func(availableBody, unavailableBody string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			body := unavailableBody
			if s.IsReady() {
				body = availableBody
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			if _, err := w.Write([]byte(body)); err != nil {
				s.logger.Error("failed to write response", zap.Error(err))
			}
		}
	}
	s.mux.HandleFunc("GET /readyz", lifecycleHandler("ready", "not ready"))
	s.mux.HandleFunc("GET /healthz", lifecycleHandler("healthy", "not healthy"))

	if m := metrics.ProductHandler(); m != nil {
		s.mux.Handle("GET /metrics", m)
	}

	if m := metrics.SystemHandler(); m != nil {
		s.mux.Handle("GET /system/metrics", m)
	}
}

func safeHandler(ll *zap.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				ll.Error("panic in handler",
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.String("remote_addr", r.RemoteAddr),
					zap.Any("panic", err),
				)
			}
		}()
		h.ServeHTTP(w, r)
	})
}
