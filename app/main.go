// http-server-projeto-korp
// Minimal HTTP service exposing the /projeto-korp endpoint and Prometheus metrics.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const serviceName = "http-server-projeto-korp"

// korpResponse is the JSON payload returned by GET /projeto-korp.
type korpResponse struct {
	Nome    string `json:"nome"`
	Horario string `json:"horario"`
}

// ---------------------------------------------------------------------------
// Prometheus metrics
// ---------------------------------------------------------------------------

var (
	// serviceUp is the availability metric: 1 = healthy, 0 = unhealthy.
	serviceUp = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "korp_service_up",
		Help: "Availability of the service: 1 when healthy, 0 when unhealthy.",
	})

	// requestsTotal is the request volume metric, sliced by method/path/status.
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "korp_http_requests_total",
		Help: "Total number of HTTP requests handled by the service.",
	}, []string{"method", "path", "status"})

	// requestDuration lets us analyse latency behaviour in Grafana.
	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "korp_http_request_duration_seconds",
		Help:    "Latency distribution of HTTP requests in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	// inFlight tracks concurrent requests being processed.
	inFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "korp_http_requests_in_flight",
		Help: "Number of HTTP requests currently being processed.",
	})

	// buildInfo exposes static metadata as a labelled gauge (common pattern).
	buildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "korp_build_info",
		Help: "Static build information about the service.",
	}, []string{"service", "version", "go_version"})
)

// healthy holds the current health state, mutated by the readiness logic.
var healthy atomic.Bool

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// statusRecorder captures the HTTP status code written by the handler.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// instrument wraps a handler with metrics collection and structured logging.
func instrument(path string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		inFlight.Inc()
		defer inFlight.Dec()

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next(recorder, r)

		elapsed := time.Since(start)
		requestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(recorder.status)).Inc()
		requestDuration.WithLabelValues(r.Method, path).Observe(elapsed.Seconds())

		slog.Info("request handled",
			"method", r.Method,
			"path", path,
			"status", recorder.status,
			"duration_ms", elapsed.Milliseconds(),
			"remote_addr", r.RemoteAddr,
		)
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// korpHandler answers GET /projeto-korp with the current UTC timestamp,
// resolved dynamically on every single request.
func korpHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, `{"erro":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	payload := korpResponse{
		Nome:    "Projeto Korp",
		Horario: time.Now().UTC().Format(time.RFC3339Nano),
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// healthzHandler is the dedicated availability endpoint used by Docker's
// healthcheck and by NGINX/Prometheus for a quick liveness signal.
func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	if !healthy.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"unhealthy"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	// Structured JSON logs make container log aggregation easier.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	listenAddr := envOrDefault("LISTEN_ADDR", ":8080")
	version := envOrDefault("APP_VERSION", "dev")

	healthy.Store(true)
	serviceUp.Set(1)
	buildInfo.WithLabelValues(serviceName, version, runtimeVersion()).Set(1)

	mux := http.NewServeMux()
	mux.HandleFunc("/projeto-korp", instrument("/projeto-korp", korpHandler))
	mux.HandleFunc("/healthz", instrument("/healthz", healthzHandler))
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	shutdownDone := make(chan struct{})
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
		<-signals

		slog.Info("shutdown signal received, draining connections")
		healthy.Store(false)
		serviceUp.Set(0)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
		}
		close(shutdownDone)
	}()

	slog.Info("starting service", "service", serviceName, "addr", listenAddr, "version", version)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server terminated unexpectedly", "error", err)
		os.Exit(1)
	}

	<-shutdownDone
	slog.Info("service stopped cleanly")
}

// envOrDefault returns the environment variable value or a fallback.
func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// runtimeVersion is isolated so main stays readable.
func runtimeVersion() string {
	return envOrDefault("GO_VERSION", "unknown")
}
