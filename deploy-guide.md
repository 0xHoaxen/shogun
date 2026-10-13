# Kubernetes + Helm Project Structure

Go microservices + web frontend, with Traefik, cert-manager, Sealed Secrets, namespaces/quotas, and Prometheus/Grafana monitoring.

> **Running on 2 vCPU / 8 GB?** The quota and resource numbers in sections 3, 8 and 9 are for a larger cluster. Use **section 13** instead.

---

## 1. Full folder structure

```
my-project/
├── services/                               # Go source code
│   ├── user-service/
│   │   ├── cmd/
│   │   ├── internal/
│   │   └── Dockerfile
│   ├── order-service/
│   └── payment-service/
├── web/                                    # Frontend source + Dockerfile
│
├── deploy/
│   ├── charts/                             # Charts YOU maintain
│   │   ├── microservice/                   # ONE generic chart for all apps
│   │   │   ├── Chart.yaml
│   │   │   ├── values.yaml                 # sane defaults
│   │   │   └── templates/
│   │   │       ├── _helpers.tpl
│   │   │       ├── deployment.yaml
│   │   │       ├── service.yaml
│   │   │       ├── ingress.yaml            # Traefik + cert-manager aware
│   │   │       ├── middleware.yaml         # optional per-service Traefik Middleware
│   │   │       ├── hpa.yaml
│   │   │       ├── pdb.yaml                # PodDisruptionBudget
│   │   │       ├── configmap.yaml
│   │   │       ├── sealedsecret.yaml
│   │   │       ├── servicemonitor.yaml     # Prometheus scrape config
│   │   │       ├── networkpolicy.yaml      # optional
│   │   │       └── serviceaccount.yaml
│   │   └── namespaces/                     # namespaces + quotas + limits
│   │       ├── Chart.yaml
│   │       └── templates/
│   │           ├── namespace.yaml
│   │           ├── resourcequota.yaml
│   │           ├── limitrange.yaml
│   │           └── networkpolicy.yaml
│   │
│   ├── apps/                               # Per-service VALUES only
│   │   ├── user-service/
│   │   │   ├── values.yaml                 # common: image repo, port, probes
│   │   │   ├── values-dev.yaml
│   │   │   ├── values-staging.yaml
│   │   │   └── values-prod.yaml            # includes sealed (encrypted) secrets
│   │   ├── order-service/
│   │   ├── payment-service/
│   │   └── web/
│   │
│   ├── infrastructure/                     # Third-party charts: values only
│   │   ├── namespaces/
│   │   │   ├── values.yaml
│   │   │   ├── values-dev.yaml
│   │   │   ├── values-staging.yaml
│   │   │   └── values-prod.yaml
│   │   ├── sealed-secrets/
│   │   │   ├── values.yaml
│   │   │   └── certs/
│   │   │       ├── dev-pub-cert.pem        # public certs, safe to commit
│   │   │       ├── staging-pub-cert.pem
│   │   │       └── prod-pub-cert.pem
│   │   ├── cert-manager/
│   │   │   ├── values.yaml
│   │   │   └── issuers/                    # plain manifests, applied after chart
│   │   │       ├── letsencrypt-staging.yaml
│   │   │       ├── letsencrypt-prod.yaml
│   │   │       └── cloudflare-sealed-secret.yaml   # only for DNS-01
│   │   ├── traefik/
│   │   │   ├── values.yaml
│   │   │   ├── values-dev.yaml
│   │   │   ├── values-prod.yaml
│   │   │   └── middlewares/                # shared: security headers, rate limit
│   │   └── monitoring/
│   │       ├── kube-prometheus-stack/
│   │       │   ├── values.yaml
│   │       │   ├── values-dev.yaml
│   │       │   └── values-prod.yaml
│   │       ├── loki-stack/values.yaml      # optional: logs
│   │       ├── dashboards/                 # Grafana dashboards (JSON / ConfigMaps)
│   │       └── alerts/                     # PrometheusRule YAMLs
│
├── helmfile.yaml.gotmpl                    # declares all releases + install order
├── scripts/
│   └── seal-secret.sh
├── .gitignore                              # ignore plain secrets
└── Makefile
```

---

## 2. Install order

Each step depends on CRDs or objects from the previous one.

| Step | Component                              | Provides                                             |
| ---- | -------------------------------------- | ---------------------------------------------------- |
| 0    | **namespaces**                         | Namespaces, quotas, LimitRanges, Pod Security labels |
| 1    | **sealed-secrets**                     | `SealedSecret` CRD                                   |
| 2    | **cert-manager**                       | `Certificate`, `ClusterIssuer` CRDs                  |
| 3    | **issuers**                            | ClusterIssuers (seal the DNS-01 token first if used) |
| 4    | **monitoring** (kube-prometheus-stack) | `ServiceMonitor`, `PrometheusRule` CRDs              |
| 5    | **traefik**                            | `Middleware`, `IngressRoute` CRDs, ingress class     |
| 6    | **apps**                               | Your Go services and web                             |

Helmfile enforces this order with `needs:` (see section 10).

---

## 3. Namespace layout

| Namespace                  | Contents                                | Notes                                      |
| -------------------------- | --------------------------------------- | ------------------------------------------ |
| `kube-system`              | sealed-secrets controller               | already exists                             |
| `cert-manager`             | cert-manager                            |                                            |
| `traefik`                  | Traefik                                 |                                            |
| `monitoring`               | Prometheus, Grafana, Alertmanager, Loki | needs relaxed Pod Security (node-exporter) |
| `dev` / `staging` / `prod` | Go services + web                       | one per environment if sharing a cluster   |

If each environment has its own cluster, use a single `apps` namespace. Only split apps further (e.g. `payments`, `core`) when teams need separate quotas or RBAC.

### Namespaces chart values

```yaml
# deploy/infrastructure/namespaces/values.yaml
namespaces:
  - name: monitoring
    podSecurity: privileged
    quota:
      requests.cpu: "4"
      requests.memory: 8Gi
      limits.memory: 12Gi
      persistentvolumeclaims: "10"
  - name: traefik
    podSecurity: baseline
    quota:
      requests.cpu: "1"
      requests.memory: 1Gi
      limits.memory: 2Gi
  - name: cert-manager
    podSecurity: restricted
    quota:
      requests.cpu: 500m
      requests.memory: 512Mi
      limits.memory: 1Gi
```

```yaml
# deploy/infrastructure/namespaces/values-prod.yaml
namespaces:
  - name: prod
    podSecurity: restricted
    defaultDenyIngress: true
    quota:
      requests.cpu: "20"
      requests.memory: 40Gi
      limits.memory: 60Gi
      pods: "100"
      services.loadbalancers: "0"
    limitRange:
      default: { memory: 512Mi }
      defaultRequest: { cpu: 100m, memory: 128Mi }
      max: { cpu: "4", memory: 4Gi }
```

### Chart templates

```yaml
# templates/namespace.yaml
{{- range .Values.namespaces }}
---
apiVersion: v1
kind: Namespace
metadata:
  name: {{ .name }}
  annotations:
    helm.sh/resource-policy: keep
  labels:
    pod-security.kubernetes.io/enforce: {{ .podSecurity | default "baseline" }}
    pod-security.kubernetes.io/warn: restricted
{{- end }}
```

```yaml
# templates/resourcequota.yaml
{{- range .Values.namespaces }}
{{- if .quota }}
---
apiVersion: v1
kind: ResourceQuota
metadata:
  name: {{ .name }}-quota
  namespace: {{ .name }}
spec:
  hard:
    {{- toYaml .quota | nindent 4 }}
{{- end }}
{{- end }}
```

```yaml
# templates/limitrange.yaml
{{- range .Values.namespaces }}
{{- if .limitRange }}
---
apiVersion: v1
kind: LimitRange
metadata:
  name: {{ .name }}-defaults
  namespace: {{ .name }}
spec:
  limits:
    - type: Container
      {{- toYaml .limitRange | nindent 6 }}
{{- end }}
{{- end }}
```

```yaml
# templates/networkpolicy.yaml
{{- range .Values.namespaces }}
{{- if .defaultDenyIngress }}
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-ingress
  namespace: {{ .name }}
spec:
  podSelector: {}
  policyTypes: [Ingress]
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-traefik-and-prometheus
  namespace: {{ .name }}
spec:
  podSelector: {}
  policyTypes: [Ingress]
  ingress:
    - from:
        - namespaceSelector:
            matchLabels: { kubernetes.io/metadata.name: traefik }
        - namespaceSelector:
            matchLabels: { kubernetes.io/metadata.name: monitoring }
        - podSelector: {}
{{- end }}
{{- end }}
```

NetworkPolicies only work if your CNI enforces them (Calico, Cilium, most managed options; not plain Flannel).

```bash
helm upgrade --install namespaces deploy/charts/namespaces \
  -f deploy/infrastructure/namespaces/values.yaml \
  -f deploy/infrastructure/namespaces/values-prod.yaml
```

---

## 4. The shared `microservice` chart

Deploying a service:

```bash
helm upgrade --install user-service deploy/charts/microservice \
  -f deploy/apps/user-service/values.yaml \
  -f deploy/apps/user-service/values-prod.yaml \
  -n prod
```

### Per-service values

```yaml
# deploy/apps/user-service/values.yaml
image:
  repository: ghcr.io/myorg/user-service
containerPort: 8080
service:
  port: 80
probes:
  liveness: /healthz
  readiness: /readyz
metrics:
  enabled: true
  path: /metrics
  serviceMonitor: true
```

```yaml
# deploy/apps/user-service/values-prod.yaml
image:
  tag: "1.4.2"
replicaCount: 3
resources:
  requests: { cpu: 200m, memory: 192Mi }
  limits: { memory: 384Mi } # memory limit, no CPU limit
env:
  GOMEMLIMIT: 340MiB # ~90% of memory limit
hpa:
  enabled: true
  minReplicas: 3
  maxReplicas: 8
  targetCPUUtilizationPercentage: 70
pdb:
  enabled: true
  minAvailable: 2
topologySpreadConstraints: true
ingress:
  enabled: true
  host: api.example.com
  clusterIssuer: letsencrypt-prod
  middlewares:
    - traefik-security-headers@kubernetescrd
sealedSecret:
  enabled: true
  encryptedData:
    DB_PASSWORD: AgBy3i4OJSWK+PiTySYZZA9rO...
    JWT_KEY: AgAKAoiQm7xFtRoV...
```

```yaml
# deploy/apps/user-service/values-dev.yaml
ingress:
  enabled: true
  host: api.dev.example.com
  clusterIssuer: letsencrypt-staging
```

### Ingress template (Traefik + cert-manager)

```yaml
# templates/ingress.yaml
{{- if .Values.ingress.enabled }}
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ include "microservice.fullname" . }}
  annotations:
    cert-manager.io/cluster-issuer: {{ .Values.ingress.clusterIssuer | default "letsencrypt-prod" }}
    traefik.ingress.kubernetes.io/router.entrypoints: websecure
    {{- with .Values.ingress.middlewares }}
    traefik.ingress.kubernetes.io/router.middlewares: {{ . | join "," }}
    {{- end }}
spec:
  ingressClassName: traefik
  tls:
    - hosts:
        - {{ .Values.ingress.host }}
      secretName: {{ include "microservice.fullname" . }}-tls
  rules:
    - host: {{ .Values.ingress.host }}
      http:
        paths:
          - path: {{ .Values.ingress.path | default "/" }}
            pathType: Prefix
            backend:
              service:
                name: {{ include "microservice.fullname" . }}
                port:
                  number: {{ .Values.service.port }}
{{- end }}
```

### SealedSecret template

```yaml
# templates/sealedsecret.yaml
{{- if .Values.sealedSecret.enabled }}
apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: {{ include "microservice.fullname" . }}-secrets
  namespace: {{ .Release.Namespace }}
spec:
  encryptedData:
    {{- toYaml .Values.sealedSecret.encryptedData | nindent 4 }}
  template:
    metadata:
      name: {{ include "microservice.fullname" . }}-secrets
      namespace: {{ .Release.Namespace }}
{{- end }}
```

Consume in `deployment.yaml`:

```yaml
{{- if .Values.sealedSecret.enabled }}
envFrom:
  - secretRef:
      name: {{ include "microservice.fullname" . }}-secrets
{{- end }}
```

---

## 5. Sealed Secrets

```bash
# Install controller
helm repo add sealed-secrets https://bitnami-labs.github.io/sealed-secrets
helm upgrade --install sealed-secrets sealed-secrets/sealed-secrets -n kube-system \
  -f deploy/infrastructure/sealed-secrets/values.yaml

# Fetch the public cert (once per cluster)
kubeseal --fetch-cert \
  --controller-name=sealed-secrets \
  --controller-namespace=kube-system > deploy/infrastructure/sealed-secrets/certs/prod-pub-cert.pem

# Seal one value (name + namespace must match the release exactly)
echo -n 's3cr3t' | kubeseal --raw --from-file=/dev/stdin \
  --cert deploy/infrastructure/sealed-secrets/certs/prod-pub-cert.pem \
  --namespace prod \
  --name user-service-secrets
```

Paste the output into `values-prod.yaml` under `sealedSecret.encryptedData`.

**Back up the controller private key (keep OUT of Git):**

```bash
kubectl get secret -n kube-system -l sealedsecrets.bitnami.com/sealed-secrets-key -o yaml > sealed-secrets-key-backup.yaml
```

### Gotchas

- Seal per environment: each cluster has its own key pair.
- Name and namespace are baked into the ciphertext (strict scope). Renaming or moving requires re-sealing.
- Install the controller before any app that renders a `SealedSecret`.
- Add plain secret files to `.gitignore`.

---

## 6. cert-manager

```yaml
# deploy/infrastructure/cert-manager/values.yaml
crds:
  enabled: true # older chart versions: installCRDs: true
prometheus:
  enabled: true
  servicemonitor:
    enabled: true
```

```bash
helm repo add jetstack https://charts.jetstack.io
helm upgrade --install cert-manager jetstack/cert-manager \
  -n cert-manager --create-namespace \
  -f deploy/infrastructure/cert-manager/values.yaml
```

```yaml
# deploy/infrastructure/cert-manager/issuers/letsencrypt-prod.yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: you@example.com
    privateKeySecretRef:
      name: letsencrypt-prod-account-key
    solvers:
      - http01:
          ingress:
            ingressClassName: traefik
```

Make `letsencrypt-staging.yaml` the same, pointing at `https://acme-staging-v02.api.letsencrypt.org/directory`. Use staging in dev (production has strict rate limits).

For wildcard certs (`*.example.com`) use a DNS-01 solver:

```yaml
solvers:
  - dns01:
      cloudflare:
        apiTokenSecretRef:
          name: cloudflare-api-token
          key: api-token
```

Store the token as a SealedSecret in `cloudflare-sealed-secret.yaml`.

---

## 7. Traefik

```yaml
# deploy/infrastructure/traefik/values.yaml
ingressClass:
  enabled: true
  isDefaultClass: true

ports:
  web:
    http:
      redirections:
        entryPoint:
          to: websecure
          scheme: https
  websecure:
    http:
      tls:
        enabled: true

providers:
  kubernetesCRD:
    enabled: true
    allowCrossNamespace: true
  kubernetesIngress:
    enabled: true

metrics:
  prometheus:
    serviceMonitor:
      enabled: true # requires Prometheus Operator CRDs (install monitoring first)

logs:
  access:
    enabled: true
```

```bash
helm repo add traefik https://traefik.github.io/charts
helm upgrade --install traefik traefik/traefik -n traefik \
  -f deploy/infrastructure/traefik/values.yaml
```

Value names change between chart major versions: check `helm show values traefik/traefik` for the version you pin.

Shared middlewares (security headers, rate limit, compression) live in `infrastructure/traefik/middlewares/`. Reference them as `<namespace>-<name>@kubernetescrd`.

### Gotchas

- cert-manager's ingress-shim only works with standard `Ingress`. With Traefik `IngressRoute`, create an explicit `Certificate`.
- DNS must point at the Traefik LoadBalancer IP before HTTP-01 can succeed.
- Debug stuck certs with `kubectl describe certificate` and `kubectl get challenges -A`.

---

## 8. Monitoring (Prometheus + Grafana)

Use the `kube-prometheus-stack` chart (Prometheus, Alertmanager, Grafana, node-exporter). Keep only values, dashboards and alert rules in the repo.

```yaml
# deploy/infrastructure/monitoring/kube-prometheus-stack/values.yaml
prometheus:
  prometheusSpec:
    serviceMonitorSelectorNilUsesHelmValues: false # discover ServiceMonitors in all namespaces
    podMonitorSelectorNilUsesHelmValues: false
    ruleSelectorNilUsesHelmValues: false
    retention: 15d
    resources:
      requests: { cpu: 500m, memory: 2Gi }
      limits: { memory: 4Gi }
grafana:
  sidecar:
    dashboards:
      enabled: true
      searchNamespace: ALL
  resources:
    requests: { cpu: 100m, memory: 256Mi }
    limits: { memory: 512Mi }
alertmanager:
  alertmanagerSpec:
    resources:
      requests: { cpu: 50m, memory: 64Mi }
      limits: { memory: 128Mi }
```

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm upgrade --install kps prometheus-community/kube-prometheus-stack -n monitoring \
  -f deploy/infrastructure/monitoring/kube-prometheus-stack/values.yaml \
  -f deploy/infrastructure/monitoring/kube-prometheus-stack/values-prod.yaml
```

- Go services expose `/metrics` via `prometheus/client_golang`; the chart's `servicemonitor.yaml` makes Prometheus scrape them.
- Dashboards: put JSON in `dashboards/` wrapped in ConfigMaps labelled `grafana_dashboard: "1"`.
- Alerts: put `PrometheusRule` YAMLs in `alerts/` (cert expiry, high error rate, OOMKills, pod restarts).
- Community dashboards worth importing: Traefik, cert-manager, Go runtime metrics.

---

## 9. Resource guidelines

**Go services**

- Memory requests equal to (or close to) limits.
- Skip CPU limits (they cause CFS throttling). Keep CPU requests for the scheduler and HPA.
- Set `GOMEMLIMIT` to about 90% of the memory limit.
- Without CPU limits `GOMAXPROCS` defaults to node cores. If you set CPU limits, use Go 1.25+ or `go.uber.org/automaxprocs`.
- Measure for a week (`container_memory_working_set_bytes`), then tighten. VPA in `updateMode: "Off"` gives recommendations.

**Quota sizing**

```
quota.requests.cpu >= sum(HPA maxReplicas x cpu request) x ~1.25
```

The headroom covers rolling updates.

**Starting points for infrastructure**

| Component             | CPU request | Memory limit |
| --------------------- | ----------- | ------------ |
| Prometheus            | 500m        | 2-4 Gi       |
| Grafana               | 100m        | 256-512 Mi   |
| Alertmanager          | 50m         | 128 Mi       |
| Traefik (x2)          | 100m each   | 256 Mi       |
| cert-manager (3 pods) | 50m each    | 128 Mi       |
| sealed-secrets        | 50m         | 128 Mi       |

The `monitoring` quota must be at least the sum of its components.

**Quota gotchas**

- If a quota covers `requests.*` or `limits.*`, every pod must declare them. LimitRange defaults rescue sidecars and third-party charts.
- Quota failures are quiet: check `kubectl describe rs` and `kubectl describe resourcequota -n prod`.
- Use looser quotas in dev but keep the LimitRange.

---

## 10. Orchestration with Helmfile (example)

```yaml
# helmfile.yaml
repositories:
  - name: sealed-secrets
    url: https://bitnami-labs.github.io/sealed-secrets
  - name: jetstack
    url: https://charts.jetstack.io
  - name: prometheus-community
    url: https://prometheus-community.github.io/helm-charts
  - name: traefik
    url: https://traefik.github.io/charts

environments:
  dev: {}
  staging: {}
  prod: {}

releases:
  - name: namespaces
    chart: ./deploy/charts/namespaces
    values:
      - deploy/infrastructure/namespaces/values.yaml
      - deploy/infrastructure/namespaces/values-{{ .Environment.Name }}.yaml

  - name: sealed-secrets
    namespace: kube-system
    chart: sealed-secrets/sealed-secrets
    needs: [namespaces]
    values: [deploy/infrastructure/sealed-secrets/values.yaml]

  - name: cert-manager
    namespace: cert-manager
    chart: jetstack/cert-manager
    needs: [namespaces]
    values: [deploy/infrastructure/cert-manager/values.yaml]

  - name: kps
    namespace: monitoring
    chart: prometheus-community/kube-prometheus-stack
    needs: [namespaces]
    values:
      - deploy/infrastructure/monitoring/kube-prometheus-stack/values.yaml
      - deploy/infrastructure/monitoring/kube-prometheus-stack/values-{{ .Environment.Name }}.yaml

  - name: traefik
    namespace: traefik
    chart: traefik/traefik
    needs: [namespaces, monitoring/kps]
    values:
      - deploy/infrastructure/traefik/values.yaml
      - deploy/infrastructure/traefik/values-{{ .Environment.Name }}.yaml

  - name: user-service
    namespace: { { .Environment.Name } }
    chart: ./deploy/charts/microservice
    needs: [sealed-secrets, cert-manager, traefik]
    values:
      - deploy/apps/user-service/values.yaml
      - deploy/apps/user-service/values-{{ .Environment.Name }}.yaml
```

Issuers (`cert-manager/issuers/*.yaml`) are applied after cert-manager, e.g. `kubectl apply -f` in a Makefile target or a small chart with a `needs: [cert-manager]` release.

Run with: `helmfile -e prod apply`

Notes:

- Helmfile v1 requires the `.gotmpl` extension when a file contains Go templating (`{{ .Environment.Name }}`), so name it `helmfile.yaml.gotmpl`.
- Preview changes first with `helmfile -e prod diff` (needs the `helm-diff` plugin).
- Deploy a single service with `helmfile -e prod -l name=user-service apply`.

### Deploying without a GitOps tool: CI pipeline

Until you adopt Argo CD or Flux, CI does the deploying:

1. **Build job** (on push to `main`): build the Go image, tag it with the git SHA, push to the registry.
2. **Deploy job**: set the image tag, then run `helmfile -e <env> -l name=<service> apply`.
   - Pass the tag without editing files: `--state-values-set imageTag=$GIT_SHA`, or `helm --set image.tag=$GIT_SHA`.
   - Or commit the new tag into `values-<env>.yaml` so Git stays the source of truth.
3. **Cluster access**: store a kubeconfig for a limited ServiceAccount as a CI secret (one per environment). Avoid cluster-admin.
4. **Protect prod**: require a manual approval on the prod deploy job.
5. **Infra changes** (`deploy/infrastructure/**`) run through a separate job that runs `helmfile diff` on pull requests and `apply` on merge.

You can adopt Argo CD later without restructuring: `deploy/apps/*` and `deploy/infrastructure/*` already hold everything it needs.

---

## 11. Makefile and .gitignore

```makefile
ENV ?= dev

infra:
	helmfile -e $(ENV) -l tier=infra apply

issuers:
	kubectl apply -f deploy/infrastructure/cert-manager/issuers/

deploy-%:
	helm upgrade --install $* deploy/charts/microservice \
	  -f deploy/apps/$*/values.yaml \
	  -f deploy/apps/$*/values-$(ENV).yaml \
	  -n $(ENV)

lint:
	helm lint deploy/charts/microservice deploy/charts/namespaces
```

```gitignore
# plain secrets: never commit
*-secret.yaml
!sealed-secret.yaml
sealed-secrets-key-backup.yaml
```

---

## 12. Quick checklist

- [ ] Namespaces, quotas and LimitRanges applied
- [ ] sealed-secrets installed, per-env public certs committed, private key backed up
- [ ] cert-manager installed, staging and prod ClusterIssuers created
- [ ] Prometheus Operator CRDs present before enabling any `ServiceMonitor`
- [ ] Traefik installed with HTTP to HTTPS redirect
- [ ] DNS points to the Traefik LoadBalancer IP
- [ ] Each service exposes `/metrics`, `/healthz`, `/readyz`
- [ ] Secrets sealed per environment with the correct name and namespace
- [ ] Dev uses `letsencrypt-staging`; prod uses `letsencrypt-prod`

---

## 13. Small cluster profile (2 vCPU / 8 GB RAM)

Use these numbers instead of the larger examples above. The goal: the sum of **requests** must fit inside what the node can actually allocate.

### Budget

| Slice                                               | CPU        | Memory      |
| --------------------------------------------------- | ---------- | ----------- |
| Node total                                          | 2000m      | 8 GB        |
| OS + kubelet / k3s + CoreDNS + metrics-server       | ~400m      | ~1.2 GB     |
| **Allocatable for workloads**                       | **~1600m** | **~6.5 GB** |
| Keep free as headroom (burst, rollouts, page cache) | ~300m      | ~1 GB       |
| **Plan to use**                                     | **~1300m** | **~5.5 GB** |

| Component                                                                         | CPU request | Memory request | Memory limit |
| --------------------------------------------------------------------------------- | ----------- | -------------- | ------------ |
| sealed-secrets                                                                    | 10m         | 32Mi           | 64Mi         |
| cert-manager (3 pods total)                                                       | 30m         | 96Mi           | 192Mi        |
| Traefik (1 replica)                                                               | 50m         | 64Mi           | 128Mi        |
| Monitoring (Prometheus + Grafana + operator + kube-state-metrics + node-exporter) | 350m        | 1.2Gi          | 1.8Gi        |
| **Infra subtotal**                                                                | **~440m**   | **~1.4Gi**     | **~2.2Gi**   |
| **Left for apps**                                                                 | **~860m**   | **~4Gi**       |              |

Each Go service typically idles at 15 to 50 MB, so 8 to 10 small services fit comfortably.

### Decisions that change on a small node

- **One environment per cluster.** Don't run dev, staging and prod together. Use this node for one (for example `prod`), and run dev locally with kind/k3d or on a separate small box.
- **Single replica everywhere.** `replicaCount: 1`, HPA `minReplicas: 1`, `maxReplicas: 2`.
- **Disable PDBs** (`pdb.enabled: false`). With one replica, `minAvailable: 1` blocks node drains and upgrades.
- **Disable `topologySpreadConstraints`.** There is only one node.
- **Skip Loki** for now. Use `kubectl logs`, or add it later with a strict memory limit.
- **Disable Alertmanager** unless you actually route alerts somewhere.
- **Run k3s rather than full kubeadm** if you have a choice. It saves about 1 GB of control-plane overhead. k3s already bundles Traefik, so either disable it (`--disable traefik`) and use your own Helm release for version control, or configure the bundled one through a `HelmChartConfig`.
- **Add 2 to 4 GB of swap or zram** if you can. It gives a safety net against OOM kills on a node this small.

### Namespaces values (small node)

```yaml
# deploy/infrastructure/namespaces/values-prod.yaml
namespaces:
  - name: prod
    podSecurity: restricted
    quota:
      requests.cpu: 900m
      requests.memory: 3500Mi
      limits.memory: 5Gi # may exceed requests; keep below ~5.5Gi
      pods: "30"
      services.loadbalancers: "0"
    limitRange:
      default: { memory: 128Mi }
      defaultRequest: { cpu: 25m, memory: 48Mi }
      max: { cpu: "1", memory: 1Gi }
```

```yaml
# deploy/infrastructure/namespaces/values.yaml (infra namespaces)
namespaces:
  - name: monitoring
    podSecurity: privileged
    quota:
      requests.cpu: 400m
      requests.memory: 1500Mi
      limits.memory: 2500Mi
      persistentvolumeclaims: "4"
  - name: traefik
    podSecurity: baseline
    quota:
      requests.cpu: 100m
      requests.memory: 128Mi
      limits.memory: 256Mi
  - name: cert-manager
    podSecurity: restricted
    quota:
      requests.cpu: 100m
      requests.memory: 256Mi
      limits.memory: 512Mi
```

### Per-service values (small node)

```yaml
# deploy/apps/user-service/values-prod.yaml
replicaCount: 1
resources:
  requests: { cpu: 50m, memory: 64Mi }
  limits: { memory: 128Mi } # still no CPU limit
env:
  GOMEMLIMIT: 115MiB # ~90% of limit
hpa:
  enabled: false # enable only for hot services (min 1, max 2)
pdb:
  enabled: false
topologySpreadConstraints: false
```

### Lean kube-prometheus-stack values

```yaml
# deploy/infrastructure/monitoring/kube-prometheus-stack/values-small.yaml
alertmanager:
  enabled: false

prometheus:
  prometheusSpec:
    retention: 5d
    retentionSize: 4GB
    scrapeInterval: 60s # default 30s doubles the load
    serviceMonitorSelectorNilUsesHelmValues: false
    podMonitorSelectorNilUsesHelmValues: false
    ruleSelectorNilUsesHelmValues: false
    resources:
      requests: { cpu: 150m, memory: 600Mi }
      limits: { memory: 1Gi }
    storageSpec:
      volumeClaimTemplate:
        spec:
          resources:
            requests:
              storage: 8Gi

grafana:
  resources:
    requests: { cpu: 30m, memory: 128Mi }
    limits: { memory: 256Mi }
  sidecar:
    dashboards:
      enabled: true
      searchNamespace: ALL

prometheus-node-exporter:
  resources:
    requests: { cpu: 10m, memory: 24Mi }
    limits: { memory: 48Mi }

kube-state-metrics:
  resources:
    requests: { cpu: 10m, memory: 48Mi }
    limits: { memory: 96Mi }

prometheusOperator:
  resources:
    requests: { cpu: 20m, memory: 64Mi }
    limits: { memory: 128Mi }

# k3s / single-node: these components are not scrapeable and just produce errors
kubeEtcd:
  enabled: false
kubeControllerManager:
  enabled: false
kubeScheduler:
  enabled: false
kubeProxy:
  enabled: false
```

Add it as a third values file in the Helmfile release:

```yaml
values:
  - deploy/infrastructure/monitoring/kube-prometheus-stack/values.yaml
  - deploy/infrastructure/monitoring/kube-prometheus-stack/values-small.yaml
```

Prometheus memory grows with the number of active series. To keep it flat, drop unused metrics and avoid high-cardinality labels (user IDs, full URLs) in your Go `client_golang` metrics.

### Traefik and cert-manager (small node)

```yaml
# traefik values-prod.yaml
deployment:
  replicas: 1
resources:
  requests: { cpu: 50m, memory: 64Mi }
  limits: { memory: 128Mi }
```

```yaml
# cert-manager values.yaml additions
resources:
  requests: { cpu: 10m, memory: 32Mi }
  limits: { memory: 96Mi }
webhook:
  resources:
    requests: { cpu: 10m, memory: 32Mi }
    limits: { memory: 64Mi }
cainjector:
  resources:
    requests: { cpu: 10m, memory: 32Mi }
    limits: { memory: 96Mi }
```

### Watch for

- **Check real numbers early:** `kubectl describe node` shows Allocatable and current requests. `kubectl top pods -A --sort-by=memory` shows actual usage.
- **Rolling updates briefly double a pod.** Set `strategy.rollingUpdate.maxSurge: 0, maxUnavailable: 1` on larger services if memory gets tight (this causes a short blip for single-replica services).
- **Helm installs can spike CPU.** Install the infrastructure in the documented order, one release at a time, not all at once.
- **Single node means no high availability.** A node reboot is downtime. Take regular backups of the sealed-secrets private key and any PersistentVolumes.
- **Outgrowing the node:** the first things to scale are Prometheus (move to a second node or remote storage) and the apps. Because everything is values-driven, moving to 2 or 3 nodes later just means restoring the larger numbers from sections 3 and 9.
