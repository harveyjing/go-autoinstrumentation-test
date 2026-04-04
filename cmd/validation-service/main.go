package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	_ "go.opentelemetry.io/auto/sdk"
)

type traceHandler struct {
	inner slog.Handler
}

func newTraceHandler(inner slog.Handler) *traceHandler {
	return &traceHandler{inner: inner}
}

func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasTraceID() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, r)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{inner: h.inner.WithGroup(name)}
}

var (
	logger *slog.Logger
	tracer = otel.Tracer("validation-service")
)

func main() {
	rand.Seed(time.Now().UnixNano())

	logger = slog.New(newTraceHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	slog.SetDefault(logger)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("POST /validate/{id}", handleValidate)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	logger.Info("starting validation service", "port", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.Start(r.Context(), "GET /healthz")
	defer span.End()

	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

func handleValidate(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "POST /validate/{id}")
	defer span.End()

	orderID := r.PathValue("id")
	delay := time.Duration(75+rand.Intn(250)) * time.Millisecond
	time.Sleep(delay)

	span.SetAttributes(
		attribute.String("order.id", orderID),
		attribute.Int64("validation.latency_ms", delay.Milliseconds()),
	)

	if rand.Intn(10) == 0 {
		span.SetStatus(codes.Error, "validation failed")
		logger.ErrorContext(ctx, "validation failed", "order_id", orderID, "reason", "random failure")
		http.Error(w, "validation failed", http.StatusInternalServerError)
		return
	}

	logger.InfoContext(ctx, "validation passed", "order_id", orderID, "latency_ms", delay.Milliseconds())
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "validated")
}
