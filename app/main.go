package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// ---------------------------------------------------------------------------
// traceHandler – slog.Handler that auto-injects trace_id / span_id from
// the OTel span context set by OBI (eBPF auto-instrumentation).
// ---------------------------------------------------------------------------

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
	// fmt.Printf("context: %+v\n", ctx)
	// Debug: print full SpanContext via fmt (not logger!) to avoid recursion
	// fmt.Fprintf(os.Stderr, "[DEBUG SpanContext] TraceID=%s SpanID=%s TraceFlags=%s IsRemote=%t IsSampled=%t IsValid=%t HasTraceID=%t HasSpanID=%t\n",
	// 	sc.TraceID(),
	// 	sc.SpanID(),
	// 	sc.TraceFlags(),
	// 	sc.IsRemote(),
	// 	sc.IsSampled(),
	// 	sc.IsValid(),
	// 	sc.HasTraceID(),
	// 	sc.HasSpanID(),
	// )

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

type Order struct {
	ID        string    `json:"id"`
	Item      string    `json:"item"`
	Quantity  int       `json:"quantity"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

var (
	orders   = make(map[string]Order)
	ordersMu sync.RWMutex
	logger   *slog.Logger
)

func main() {
	// Wrap JSONHandler with traceHandler so every log line gets trace_id/span_id
	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug, // verbose logging
	})
	logger = slog.New(newTraceHandler(jsonHandler))
	slog.SetDefault(logger)

	logger.Info("initializing trace-aware structured logger",
		"handler", "traceHandler(JSONHandler)",
		"level", "DEBUG",
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /orders", handleListOrders)
	mux.HandleFunc("POST /orders", handleCreateOrder)
	mux.HandleFunc("GET /orders/{id}", handleGetOrder)
	mux.HandleFunc("POST /orders/{id}/validate", handleValidateOrder)

	logger.Info("registered HTTP routes",
		"routes", []string{
			"GET /healthz",
			"GET /orders",
			"POST /orders",
			"GET /orders/{id}",
			"POST /orders/{id}/validate",
		},
	)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	logger.Info("starting order service", "port", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

// add a helper function to generate random delay to simulate processing time and create more interesting traces
func randomDelay() {
	delay := time.Duration(rand.Intn(500)) * time.Millisecond
	// fmt.Printf("Simulating processing delay: %v\n", delay)
	time.Sleep(delay)
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	// fmt.Printf("headers: %+v\n", r.Header)
	// span := trace.SpanFromContext(r.Context())

	// fmt.Printf("SpanContext: TraceID=%s SpanID=%s TraceFlags=%s IsRemote=%t IsSampled=%t IsValid=%t HasTraceID=%t HasSpanID=%t\n",
	// 	span.SpanContext().TraceID(),
	// 	span.SpanContext().SpanID(),
	// 	span.SpanContext().TraceFlags(),
	// 	span.SpanContext().IsRemote(),
	// 	span.SpanContext().IsSampled(),
	// 	span.SpanContext().IsValid(),
	// 	span.SpanContext().HasTraceID(),
	// 	span.SpanContext().HasSpanID(),
	// )

	// if span.SpanContext().IsValid() {

	// 	// // Add to your structured logger
	// 	// logger := slog.With(
	// 	// 	"trace_id", span.SpanContext().TraceID().String(),
	// 	// 	"span_id", span.SpanContext().SpanID().String(),
	// 	// )
	// 	// Attach logger to context or use directly
	// 	logger.Info("handling request", "path", r.URL.Path)
	// }
	randomDelay() // simulate some processing time
	w.WriteHeader(http.StatusOK)
	fmt.Printf(`{"level":"INFO","msg":"raw test"}`)
	fmt.Fprintln(w, "ok")
}

func handleListOrders(w http.ResponseWriter, r *http.Request) {
	ordersMu.RLock()
	list := make([]Order, 0, len(orders))
	for _, o := range orders {
		list = append(list, o)
		randomDelay() // simulate processing time for each order to create more interesting traces
	}
	ordersMu.RUnlock()

	slog.Info("listing orders", "count", len(list))
	writeJSON(w, http.StatusOK, list)
}

func handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Item     string `json:"item"`
		Quantity int    `json:"quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		logger.ErrorContext(r.Context(), "invalid request body", "error", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	order := Order{
		ID:        fmt.Sprintf("ord-%d", time.Now().UnixNano()),
		Item:      input.Item,
		Quantity:  input.Quantity,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	logger.InfoContext(r.Context(), "creating order", "order_id", order.ID, "item", order.Item, "quantity", order.Quantity)

	// Call validate endpoint internally — creates a child span in traces
	validateURL := fmt.Sprintf("http://localhost:%s/orders/%s/validate", getPort(), order.ID)
	resp, err := http.Post(validateURL, "application/json", nil)
	if err != nil {
		logger.ErrorContext(r.Context(), "validation call failed", "order_id", order.ID, "error", err)
		order.Status = "validation_error"
	} else {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			order.Status = "validated"
		} else {
			order.Status = "validation_failed"
		}
	}

	ordersMu.Lock()
	orders[order.ID] = order
	ordersMu.Unlock()

	logger.InfoContext(r.Context(), "order created", "order_id", order.ID, "status", order.Status)
	writeJSON(w, http.StatusCreated, order)
}

func handleGetOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	ordersMu.RLock()
	order, ok := orders[id]
	ordersMu.RUnlock()

	if !ok {
		logger.WarnContext(r.Context(), "order not found", "order_id", id)
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}

	logger.InfoContext(r.Context(), "fetched order", "order_id", id)
	writeJSON(w, http.StatusOK, order)
}

func handleValidateOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Simulate processing latency
	delay := time.Duration(50+rand.Intn(200)) * time.Millisecond
	time.Sleep(delay)

	// ~10% chance of failure for interesting error traces
	if rand.Intn(10) == 0 {
		logger.ErrorContext(r.Context(), "validation failed", "order_id", id, "reason", "random failure")
		http.Error(w, "validation failed", http.StatusInternalServerError)
		return
	}

	logger.InfoContext(r.Context(), "order validated", "order_id", id, "latency_ms", delay.Milliseconds())
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "validated")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func getPort() string {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return port
}
