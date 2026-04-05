package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	_ "go.opentelemetry.io/auto/sdk"
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
	tracer   = otel.Tracer("order-service")
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
	_, span := tracer.Start(r.Context(), "GET /healthz")
	defer span.End()

	randomDelay() // simulate some processing time
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

func handleListOrders(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "GET /orders")
	defer span.End()

	// Step 1 – fetch all orders from the in-memory store
	list := fetchOrders(ctx)
	span.SetAttributes(attribute.Int("order.count", len(list)))

	// Step 2 – enrich each order (simulate DB / external lookup per order)
	for i := range list {
		list[i] = enrichOrder(ctx, list[i])
	}

	// Step 3 – filter & sort the order list
	filtered := filterOrders(ctx, list)
	span.SetAttributes(attribute.Int("order.filtered_count", len(filtered)))

	// Step 4 – build the JSON response (with occasional simulated error)
	data, err := buildResponse(ctx, filtered)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to build response")
		logger.ErrorContext(ctx, "buildResponse failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	slog.InfoContext(ctx, "listing orders", "count", len(list), "filtered", len(filtered))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// ---------------------------------------------------------------------------
// Child-span helpers used by handleListOrders for richer trace trees
// ---------------------------------------------------------------------------

// fetchOrders reads all orders from the in-memory store.
func fetchOrders(ctx context.Context) []Order {
	ctx, span := tracer.Start(ctx, "fetchOrders")
	defer span.End()

	start := time.Now()

	ordersMu.RLock()
	list := make([]Order, 0, len(orders))
	for _, o := range orders {
		list = append(list, o)
	}
	ordersMu.RUnlock()

	// simulate datastore latency
	delay := time.Duration(20+rand.Intn(80)) * time.Millisecond
	time.Sleep(delay)

	span.SetAttributes(
		attribute.Int("db.result_count", len(list)),
		attribute.Int64("db.latency_ms", time.Since(start).Milliseconds()),
	)
	logger.InfoContext(ctx, "fetched orders from store", "count", len(list))
	return list
}

// enrichOrder simulates looking up extra data for a single order
// (e.g. warehouse stock, customer info). Each call creates its own span.
func enrichOrder(ctx context.Context, o Order) Order {
	ctx, span := tracer.Start(ctx, "enrichOrder")
	defer span.End()

	span.SetAttributes(
		attribute.String("order.id", o.ID),
		attribute.String("order.item", o.Item),
	)

	// simulate enrichment latency (external service / cache)
	delay := time.Duration(10+rand.Intn(60)) * time.Millisecond
	time.Sleep(delay)

	// ~8 % chance of enrichment warning (non-fatal)
	if rand.Intn(12) == 0 {
		span.AddEvent("enrichment_degraded", trace.WithAttributes(
			attribute.String("reason", "cache miss – fell back to slow path"),
		))
		logger.WarnContext(ctx, "enrichment degraded", "order_id", o.ID)
		delay += time.Duration(50+rand.Intn(100)) * time.Millisecond
		time.Sleep(delay)
	}

	span.SetAttributes(attribute.Int64("enrich.latency_ms", delay.Milliseconds()))
	logger.DebugContext(ctx, "enriched order", "order_id", o.ID, "latency_ms", delay.Milliseconds())
	return o
}

// filterOrders simulates filtering and sorting the order list
// (e.g. by status, date range, pagination).
func filterOrders(ctx context.Context, list []Order) []Order {
	ctx, span := tracer.Start(ctx, "filterOrders")
	defer span.End()

	before := len(list)

	// simulate filter processing time
	delay := time.Duration(15+rand.Intn(50)) * time.Millisecond
	time.Sleep(delay)

	// randomly drop ~20 % of orders to simulate a real filter
	filtered := make([]Order, 0, len(list))
	for _, o := range list {
		if rand.Intn(5) != 0 { // keep ~80 %
			filtered = append(filtered, o)
		}
	}

	// span.SetAttributes(
	// 	attribute.Int("filter.input_count", before),
	// 	attribute.Int("filter.output_count", len(filtered)),
	// 	attribute.Int64("filter.latency_ms", delay.Milliseconds()),
	// )
	logger.InfoContext(ctx, "filtered orders", "before", before, "after", len(filtered))
	return filtered
}

// buildResponse marshals the order list to JSON. ~5 % of the time it
// injects a simulated serialisation error so error traces appear in Tempo.
func buildResponse(ctx context.Context, list []Order) ([]byte, error) {
	_, span := tracer.Start(ctx, "buildResponse")
	defer span.End()

	// simulate marshalling time
	delay := time.Duration(5+rand.Intn(30)) * time.Millisecond
	time.Sleep(delay)

	// ~5 % chance of a simulated error
	if rand.Intn(20) == 0 {
		err := fmt.Errorf("simulated serialisation failure")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.ErrorContext(ctx, "buildResponse error", "error", err)
		return nil, err
	}

	data, err := json.Marshal(list)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "json marshal failed")
		return nil, err
	}

	span.SetAttributes(
		attribute.Int("response.size_bytes", len(data)),
		attribute.Int("response.order_count", len(list)),
		attribute.Int64("response.latency_ms", delay.Milliseconds()),
	)
	return data, nil
}

func handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "POST /orders")
	defer span.End()

	var input struct {
		Item     string `json:"item"`
		Quantity int    `json:"quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid request body")
		logger.ErrorContext(ctx, "invalid request body", "error", err)
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

	span.SetAttributes(
		attribute.String("order.id", order.ID),
		attribute.String("order.item", order.Item),
		attribute.Int("order.quantity", order.Quantity),
	)

	logger.InfoContext(ctx, "creating order", "order_id", order.ID, "item", order.Item, "quantity", order.Quantity)

	status, err := callValidationService(ctx, order.ID)
	if err != nil {
		span.RecordError(err)
		logger.ErrorContext(ctx, "validation call failed", "order_id", order.ID, "error", err)
		order.Status = "validation_error"
	} else {
		if status == http.StatusOK {
			order.Status = "validated"
		} else {
			order.Status = "validation_failed"
		}
	}

	ordersMu.Lock()
	orders[order.ID] = order
	ordersMu.Unlock()

	span.SetAttributes(attribute.String("order.status", order.Status))
	logger.InfoContext(ctx, "order created", "order_id", order.ID, "status", order.Status)
	writeJSON(w, http.StatusCreated, order)
}

func handleGetOrder(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "GET /orders/{id}")
	defer span.End()

	id := r.PathValue("id")
	span.SetAttributes(attribute.String("order.id", id))

	ordersMu.RLock()
	order, ok := orders[id]
	ordersMu.RUnlock()

	if !ok {
		span.SetStatus(codes.Error, "order not found")
		logger.WarnContext(ctx, "order not found", "order_id", id)
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}

	logger.InfoContext(ctx, "fetched order", "order_id", id)
	writeJSON(w, http.StatusOK, order)
}

func handleValidateOrder(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "POST /orders/{id}/validate")
	defer span.End()

	id := r.PathValue("id")
	span.SetAttributes(attribute.String("order.id", id))

	// Simulate processing latency
	delay := time.Duration(50+rand.Intn(200)) * time.Millisecond
	time.Sleep(delay)

	// ~10% chance of failure for interesting error traces
	if rand.Intn(10) == 0 {
		span.SetStatus(codes.Error, "validation failed")
		logger.ErrorContext(ctx, "validation failed", "order_id", id, "reason", "random failure")
		http.Error(w, "validation failed", http.StatusInternalServerError)
		return
	}

	span.SetAttributes(attribute.Int64("validate.latency_ms", delay.Milliseconds()))
	logger.InfoContext(ctx, "order validated", "order_id", id, "latency_ms", delay.Milliseconds())
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

func callValidationService(ctx context.Context, orderID string) (int, error) {
	ctx, span := tracer.Start(ctx, "callValidationService")
	defer span.End()

	validateURL := fmt.Sprintf("%s/validate/%s", strings.TrimRight(getValidationServiceURL(), "/"), url.PathEscape(orderID))
	span.SetAttributes(
		attribute.String("validation.url", validateURL),
		attribute.String("order.id", orderID),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, validateURL, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to build validation request")
		return 0, err
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "validation request failed")
		return 0, err
	}
	defer resp.Body.Close()

	span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))
	if resp.StatusCode >= http.StatusBadRequest {
		span.SetStatus(codes.Error, fmt.Sprintf("validation returned %d", resp.StatusCode))
	}

	return resp.StatusCode, nil
}

func getValidationServiceURL() string {
	if baseURL := os.Getenv("VALIDATION_SERVICE_URL"); baseURL != "" {
		return baseURL
	}
	return "http://validation-service.demo.svc.cluster.local"
}
