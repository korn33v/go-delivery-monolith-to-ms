package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func Context() (context.Context, context.CancelFunc) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func Run(ctx context.Context, addr string, handler http.Handler) error {
	s := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	result := make(chan error, 1)
	go func() { slog.Info("listening", "address", addr); result <- s.ListenAndServe() }()
	select {
	case err := <-result:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.Shutdown(shutdownCtx)
	}
	return nil
}
