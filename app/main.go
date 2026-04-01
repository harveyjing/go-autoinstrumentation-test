package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"
)

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
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /orders", handleListOrders)
	mux.HandleFunc("POST /orders", handleCreateOrder)
	mux.HandleFunc("GET /orders/{id}", handleGetOrder)
	mux.HandleFunc("POST /orders/{id}/validate", handleValidateOrder)

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

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(os.Stdout, `{"level":"INFO","msg":"raw test"}`)
	fmt.Fprintln(w, "ok")
}

func handleListOrders(w http.ResponseWriter, r *http.Request) {
	ordersMu.RLock()
	list := make([]Order, 0, len(orders))
	for _, o := range orders {
		list = append(list, o)
	}
	ordersMu.RUnlock()

	logger.Info("listing orders", "count", len(list))
	writeJSON(w, http.StatusOK, list)
}

func handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Item     string `json:"item"`
		Quantity int    `json:"quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		logger.Error("invalid request body", "error", err)
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

	logger.Info("creating order", "order_id", order.ID, "item", order.Item, "quantity", order.Quantity)

	// Call validate endpoint internally — creates a child span in traces
	validateURL := fmt.Sprintf("http://localhost:%s/orders/%s/validate", getPort(), order.ID)
	resp, err := http.Post(validateURL, "application/json", nil)
	if err != nil {
		logger.Error("validation call failed", "order_id", order.ID, "error", err)
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

	logger.Info("order created", "order_id", order.ID, "status", order.Status)
	writeJSON(w, http.StatusCreated, order)
}

func handleGetOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	ordersMu.RLock()
	order, ok := orders[id]
	ordersMu.RUnlock()

	if !ok {
		logger.Warn("order not found", "order_id", id)
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}

	logger.Info("fetched order", "order_id", id)
	writeJSON(w, http.StatusOK, order)
}

func handleValidateOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Simulate processing latency
	delay := time.Duration(50+rand.Intn(200)) * time.Millisecond
	time.Sleep(delay)

	// ~10% chance of failure for interesting error traces
	if rand.Intn(10) == 0 {
		logger.Error("validation failed", "order_id", id, "reason", "random failure")
		http.Error(w, "validation failed", http.StatusInternalServerError)
		return
	}

	logger.Info("order validated", "order_id", id, "latency_ms", delay.Milliseconds())
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
