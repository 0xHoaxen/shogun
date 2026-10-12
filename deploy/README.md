# deploy/

Everything that builds, runs or ships Shogun. Pick the folder by the question you have.

| Folder | What it holds | Used by |
| --- | --- | --- |
| `docker/` | `Dockerfile` (all Go services, `ARG SERVICE`) and `web.Dockerfile` | CI, `release.yml` |
| `compose/` | the local stack, its observability profile, the Postgres bootstrap SQL and the backup script | `make up`, e2e tests |
| `charts/` | the charts we maintain: `microservice` (every app), `postgres` (with the nightly backup), `namespaces` (quotas, limits), `monitoring-extras` (Shogun's alert rules and dashboard) | `helmfile.yaml.gotmpl` |
| `apps/<name>/` | per-app values only: `values.yaml` (what the app is), `values-staging.yaml`, `values-production.yaml` (`image.tag`, bumped by `deploy.yml`); `common.yaml` for every app (peers, pull secret) | the `apps` phase |
| `infrastructure/<component>/` | values for everything that is not an app: `namespaces`, `postgres`, `sealed-secrets` (with the per-env certificates and sealed Secrets), `cert-manager` (with the ClusterIssuers), `traefik` (the k3s redirect), `monitoring/kube-prometheus-stack` | the other phases |

`helmfile.yaml.gotmpl` at the repo root declares every release, its values and its install order. `shogun-deploy` (`scripts/deploy/`, run with `uv run --project scripts/deploy shogun-deploy` or the `make deploy*` targets) applies it phase by phase.

## Rules of thumb

- A chart we write goes in `charts/`. A third-party chart gets only values, in `infrastructure/<component>/`, with its version pinned in the helmfile.
- Per-environment settings go in `values-<env>.yaml`, never in a chart's own `values.yaml`.
- A new service needs only values: `make new-service` runs `scripts/helm.py add`, which writes `apps/<name>/` and adds it to the peers in `apps/common.yaml`.
- The alert rules, the dashboard, the Postgres bootstrap SQL and the backup script have one copy, under `compose/`. The charts link to it with symlinks.
- An app without `values-<env>.yaml` is not part of that environment (web has no production values yet).

## Install order on a new cluster

Staging and production are separate clusters, each with its own sealing key.

| Phase | What | |
| --- | --- | --- |
| `bootstrap` | namespaces, quotas and limits; the sealed-secrets controller | helmfile |
| `secrets` | the sealed Secrets in `infrastructure/sealed-secrets/secrets/<env>/` | `kubectl apply -k` |
| `platform` | cert-manager; kube-prometheus-stack (release `monitoring`); `monitoring-extras` | helmfile |
| `ingress` | the ClusterIssuers and the k3s Traefik redirect | `kubectl apply -f` |
| `data` | Postgres and its backup CronJob | helmfile |
| `apps` | `soroban`, then the other nine services, then web | helmfile |

```sh
make tools                                         # pinned helmfile and helm-diff, among others
make deploy ENV=staging CONTEXT=<ctx>              # stops after bootstrap until Secrets are sealed (or offers to seal)
shogun-deploy secrets fetch-cert --env staging --context <ctx>
shogun-deploy secrets backup-key --env staging --context <ctx>
make seal ENV=staging                              # from .secrets/staging.env; commit infrastructure/sealed-secrets
shogun-deploy up --env staging --context <ctx> --from secrets
```

After that, `make deploy-plan` shows what would change, `shogun-deploy apps <name> ...` upgrades apps only, `make deploy-status` shows releases, pods and the last backup. Every run logs to `.deploy-logs/`. The full runbook is `docs/deploy.md`.
