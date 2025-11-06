package status

import (
	"context"
	"errors"
	"net"
	"net/http"
	_ "net/http/pprof"
	"time"

	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
	"go.uber.org/zap"
)

type StatusServer interface {
	Start() error
	Stop() error
	IsReady() bool
}

type BaseStatusServer struct {
	listen     string
	logger     *zap.Logger
	server     *http.Server
	readyCheck func() bool
	mux        *http.ServeMux

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
	s.server = &http.Server{
		// set a base context for graceful cancellation of in-flight requests
		// contexts passed to handlers will be derived from this base context
		BaseContext: func(l net.Listener) context.Context {
			return s.ctx
		},
		Addr:    s.listen,
		Handler: safeHandler(s.logger, s.mux),
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("unable to start status server", zap.Error(err))
		}
	}()

	s.logger.Info("status server listening", zap.String("url", "http://"+s.listen))
	return nil
}

func (s *BaseStatusServer) Stop() error {
	// cancel the base context to gracefully cancel in-flight requests
	s.cancel(errors.New("server shutdown"))

	// shutdown the server with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

func (s *BaseStatusServer) IsReady() bool {
	return s.readyCheck()
}

func (s *BaseStatusServer) setupRoutes() {
	s.mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if s.IsReady() {
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte("ready")); err != nil {
				s.logger.Error("failed to write response", zap.Error(err))
			}
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			if _, err := w.Write([]byte("not ready")); err != nil {
				s.logger.Error("failed to write response", zap.Error(err))
			}
		}
	})

	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("healthy")); err != nil {
			s.logger.Error("failed to write response", zap.Error(err))
		}
	})

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
