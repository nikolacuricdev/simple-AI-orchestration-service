// Package httpx holds the HTTP server boilerplate both binaries share:
// timeouts, and shutting down without dropping in-flight requests.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// shutdownGrace is how long in-flight requests get to finish on SIGTERM.
const shutdownGrace = 10 * time.Second

// ListenAndServe runs an HTTP server until ctx is cancelled, then drains it.
//
// The explicit timeouts matter: http.ListenAndServe leaves all of them at zero,
// so a single slow or idle client can hold a connection open indefinitely.
// WriteTimeout is a parameter because the agent service needs a long one — a
// local model can think for a while — while the MCP server does not.
func ListenAndServe(ctx context.Context, addr string, handler http.Handler, writeTimeout time.Duration, log *slog.Logger) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       60 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
			return
		}
		errs <- nil
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		log.Info("shutting down", "grace", shutdownGrace.String())
	}

	// A fresh context: ctx is already cancelled, and Shutdown needs its own
	// deadline to bound the drain.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return <-errs
}

// Retry calls fn until it succeeds, ctx is cancelled, or attempts run out.
//
// Startup dependencies in Compose come up in an unpredictable order, and a
// service that crashes because a peer is not ready yet is a service that needs
// babysitting. Retrying is cheaper than orchestrating health gates.
func Retry(ctx context.Context, attempts int, delay time.Duration, log *slog.Logger, what string, fn func(context.Context) error) error {
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err = fn(ctx); err == nil {
			return nil
		}
		log.Warn("waiting for dependency", "what", what, "attempt", attempt, "of", attempts, "error", err)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return fmt.Errorf("%s not ready after %d attempts: %w", what, attempts, err)
}
