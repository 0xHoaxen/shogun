# deploy/

Everything that builds, runs or ships Shogun. Pick the folder by the question you have.

| Folder | What it holds | Used by |
| --- | --- | --- |
| `docker/` | `Dockerfile` (all Go services, `ARG SERVICE`) and `web.Dockerfile` | CI, `release.yml` |
| `compose/` | the local stack, its observability profile, Postgres bootstrap SQL | `make up`, e2e tests |
| `helm/apps/` | charts for what we ship: ten services, `web`, `postgres`, and the `service` library chart | `scripts/deploy.sh`, `release.yml` |
| `helm/platform/` | charts installed once per cluster: `ingress` (Traefik redirect, Let's Encrypt issuers) | `scripts/platform.sh` |
| `helm/monitoring/` | the observability chart: alert rules and the Grafana dashboard | `scripts/platform.sh` |
| `helm/values/<env>/` | per-service values for `staging` and `production`; `image.tag` is bumped here by `deploy.yml` | `scripts/deploy.sh`, `deploy.yml` |
| `helm/values/cluster/` | values for the upstream charts that `platform.sh` installs | `scripts/platform.sh` |

## Rules of thumb

- A chart that ships with a release goes in `helm/apps/`. A chart you install once and forget goes in `helm/platform/`.
- Per-environment settings go in `helm/values/<env>/`, never in a chart's own `values.yaml`.
- Service charts are generated: change `helm/apps/service/` and run `scripts/helm.py sync`.
- The alert rules, the dashboard and the Postgres bootstrap SQL have one copy, under `compose/`. The charts link to it with symlinks.

## Install order on a new cluster

1. `scripts/k8s-secrets.sh --context <ctx> --namespace <ns>`: Secrets for Postgres, the services and Grafana (Grafana reads its login at install).
2. `scripts/platform.sh --context <ctx>`: cert-manager, ingress, Prometheus stack, monitoring.
3. `scripts/deploy.sh --context <ctx> --namespace <ns> --env <env>`: Postgres, the services, then web.

The full runbook is `docs/deploy.md`.
