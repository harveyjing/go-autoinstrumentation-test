# Repository Guidelines

## Project Structure & Module Organization
This repository combines a small Go service with Kubernetes and Helm configuration for an OpenTelemetry and Grafana demo stack. Run above the k3s.

- `cmd/order-service/` and `cmd/validation-service/`: Go services, each with its own `main.go`, `go.mod`, and `Dockerfile`.
- `deploy/k8s/`: raw Kubernetes manifests such as `order-service.yaml`, `validation-service.yaml`, `otel-collector.yaml`, and `instrumentation.yaml`.
- `deploy/helm/`: values files for Grafana, Tempo, Loki, Prometheus, Alloy, the OTel Operator, and OBI.
- `README.md`: bootstrap commands for local image loading and cluster deployment.

## Build, Test, and Development Commands
- `cd cmd/order-service && go run .`: run the order service locally on port `8080` by default.
- `cd cmd/order-service && go build ./...`: compile the order service and catch module issues.
- `cd cmd/validation-service && go build ./...`: compile the downstream validation service.
- `cd cmd/order-service && docker buildx build --platform=linux/amd64 -t order-service:latest .`: build the order-service image.
- `kubectl apply -f deploy/k8s/order-service.yaml`: deploy the main service manifest.
- `helm upgrade ... -f deploy/helm/<name>-values.yaml --install`: install or update observability components using the repo’s pinned values files.
