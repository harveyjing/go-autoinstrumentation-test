
# OpenTelemetry Microservice Trace Demo

This repo demonstrates tracing across two Go services in Kubernetes:

- `order-service`: entrypoint service
- `validation-service`: downstream service called by `order-service`

## Build and load images into k3s

```bash
cd cmd/order-service && docker buildx build --platform=linux/amd64 -t order-service:latest .
docker save -o order-service.tar order-service:latest
k3s ctr images import order-service.tar

cd ../validation-service && docker buildx build --platform=linux/amd64 -t validation-service:latest .
docker save -o validation-service.tar validation-service:latest
k3s ctr images import validation-service.tar
```

## Install observability components

```bash
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo add grafana https://grafana.github.io/helm-charts
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
```

```bash
helm upgrade opentelemetry-operator open-telemetry/opentelemetry-operator -n opentelemetry-operator-system \
  -f deploy/helm/otel-operator-values.yaml --create-namespace --install
kubectl apply -f deploy/k8s/otel-collector.yaml
kubectl apply -f deploy/k8s/instrumentation.yaml
```

```bash
helm upgrade tempo grafana/tempo -n observability -f deploy/helm/tempo-values.yaml --install
helm upgrade loki grafana/loki -n observability -f deploy/helm/loki-values.yaml --install
helm upgrade prometheus prometheus-community/prometheus -n observability -f deploy/helm/prometheus-values.yaml --install
helm upgrade grafana grafana/grafana -n observability -f deploy/helm/grafana-values.yaml --install
helm upgrade alloy grafana/alloy -n observability -f deploy/helm/alloy-values.yaml --install
```

## Deploy demo services

```bash
kubectl apply -f deploy/k8s/order-service.yaml
kubectl apply -f deploy/k8s/validation-service.yaml
```

## Generate a distributed trace

```bash
curl -X POST http://localhost:30374/orders \
  -H 'Content-Type: application/json' \
  -d '{"item":"coffee","quantity":2}'
```

The request should create a trace with spans from both `order-service` and `validation-service` in Tempo.

## Optional: install OpenTelemetry eBPF instrumentation

```bash
helm upgrade obi open-telemetry/opentelemetry-ebpf-instrumentation \
  -n observability -f deploy/helm/obi-values.yaml --install
```
