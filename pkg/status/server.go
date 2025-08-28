package status

import (
	"context"
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
}

func NewBaseStatusServer(listen string, logger *zap.Logger, readyCheck func() bool) *BaseStatusServer {
	return &BaseStatusServer{
		listen:     listen,
		logger:     logger,
		readyCheck: readyCheck,
	}
}

func (s *BaseStatusServer) Start() error {
	mux := http.DefaultServeMux
	s.setupRoutes(mux)

	s.server = &http.Server{
		Addr:    s.listen,
		Handler: mux,
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

func (s *BaseStatusServer) IsReady() bool {
	return s.readyCheck()
}

func (s *BaseStatusServer) setupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("healthy")); err != nil {
			s.logger.Error("failed to write response", zap.Error(err))
		}
	})

	if m := metrics.ProductHandler(); m != nil {
		mux.Handle("GET /metrics", m)
	}

	if m := metrics.SystemHandler(); m != nil {
		mux.Handle("GET /system/metrics", m)
	}
}
