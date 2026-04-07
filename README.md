
# OpenTelemetry Microservice Trace Demo

This repo demonstrates tracing across Go services behind an nginx gateway in Kubernetes:

- `nginx-gateway`: entry point; auto-instrumented via the OTel Operator (`inject-nginx`)
- `order-service`: upstream Go service called by nginx
- `validation-service`: downstream Go service called by `order-service`

## Build and load images into k3s

```bash
cd cmd/order-service && docker buildx build --platform=linux/amd64 -t order-service:latest .
docker save -o order-service.tar order-service:latest
k3s ctr images import order-service.tar

cd ../validation-service && docker buildx build --platform=linux/amd64 -t validation-service:latest .
docker save -o validation-service.tar validation-service:latest
k3s ctr images import validation-service.tar
```

## Deploy demo services

```bash
kubectl apply -f deploy/k8s/order-service.yaml
kubectl apply -f deploy/k8s/validation-service.yaml
```

## Deploy nginx gateway

The nginx gateway uses the OTel Operator's native nginx auto-instrumentation — no custom image required.
The operator injects an init container that installs the OTel webserver module and patches the nginx config.

```bash
kubectl apply -f deploy/k8s/nginx-gateway.yaml
```

Verify the instrumentation init container completed:

```bash
kubectl describe pod -n demo -l app=nginx-gateway
# Expect: Init Container "opentelemetry-auto-instrumentation-nginx" → State: Terminated (Completed)
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


## Generate a distributed trace

```bash
curl -X POST http://localhost:30374/orders \
  -H 'Content-Type: application/json' \
  -d '{"item":"coffee","quantity":2}'
```

The request flows through nginx → order-service → validation-service.
In Grafana Tempo the trace should show a root `nginx-gateway` span with child spans from both Go services.

## Optional: install OpenTelemetry eBPF instrumentation

```bash
helm upgrade obi open-telemetry/opentelemetry-ebpf-instrumentation \
  -n observability -f deploy/helm/obi-values.yaml --install
```
