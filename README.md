

```bash
cd app && docker buildx build --platform=linux/amd64 -t order-service:latest .
ctr image import order-service:latest
kubectl apply -f k8s/order-service.yaml
```

```bash
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo add grafana https://grafana.github.io/helm-charts
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
```

```bash
helm upgrade my-opentelemetry-operator open-telemetry/opentelemetry-operator -n observability \
  -f helm/otel-operator-values.yaml --create-namespace --install
kubectl apply -f k8s/otel-collector.yaml
```

```bash
helm upgrade tempo grafana/tempo -n observability -f helm/tempo-values.yaml --install
helm upgrade loki grafana/loki -n observability -f helm/loki-values.yaml --install
helm upgrade prometheus prometheus-community/prometheus -n observability -f helm/prometheus-values.yaml --install
helm upgrade grafana grafana/grafana -n observability -f helm/grafana-values.yaml --install
helm upgrade alloy grafana/alloy -n observability -f helm/alloy-values.yaml --install
```

```bash
helm upgrade obi open-telemetry/opentelemetry-ebpf-instrumentation \
  -n observability -f helm/obi-values.yaml --install
```