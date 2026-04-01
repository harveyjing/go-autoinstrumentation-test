

```bash
cd app && docker buildx build --platform=linux/amd64 -t order-service:latest .
ctr image import order-service:latest
kubectl apply -f k8s/order-service.yaml
```

```bash
helm repo add  open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo add grafana https://grafana.github.io/helm-charts
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
```

```bash
helm install my-opentelemetry-operator open-telemetry/opentelemetry-operator -n observability \
  -f helm/otel-operator-values.yaml --create-namespace
kubectl apply -f k8s/otel-collector.yaml
```

```bash
helm install tempo grafana/tempo -n observability -f helm/tempo-values.yaml
helm install loki grafana/loki -n observability -f helm/loki-values.yaml
helm install prometheus prometheus-community/prometheus -n observability -f helm/prometheus-values.yaml
helm install grafana grafana/grafana -n observability -f helm/grafana-values.yaml
helm install alloy grafana/alloy -n observability -f helm/alloy-values.yaml
```

```bash
helm install obi open-telemetry/opentelemetry-ebpf-instrumentation \
  -n observability -f helm/obi-values.yaml
```