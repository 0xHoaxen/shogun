# deploy/

Everything that builds, runs or ships Shogun. Pick the folder by the question you have.

| Folder | What it holds | Used by |
| --- | --- | --- |
| `docker/` | `Dockerfile` (all Go services, `ARG SERVICE`) and `web.Dockerfile` | CI, `release.yml` |
| `compose/` | the local stack, its observability profile, the Postgres bootstrap SQL and the backup script | `make up`, e2e tests |
| `charts/` | the charts we maintain: `microservice` (every app), `postgres` (with the nightly backup), `namespaces` (quotas, limits), `monitoring-extras` (Shogun's alert rules and dashboard) | `helmfile.yaml.gotmpl` |
| `apps/<name>/` | per-app values only: `values.yaml` (what the app is), `values-staging.yaml`, `values-production.yaml` (`image.tag`, bumped by `deploy.yml`); `common.yaml` for every app (peers, pull secret) | the `apps` phase |
| `infrastructure/<component>/` | values for everything that is not an app: `namespaces`, `postgres`, `sealed-secrets` (with the per-env certificates and sealed Secrets), `cert-manager` (with the ClusterIssuers), `traefik` (the k3s redirect), `monitoring/kube-prometheus-stack` | the other phases |

`helmfile.yaml.gotmpl` at the repo root declares every release, its values and its install order (`needs`). The `make deploy*` targets run `helmfile` with the environment and context you give them; nothing else sits in between.

## Rules of thumb

- A chart we write goes in `charts/`. A third-party chart gets only values, in `infrastructure/<component>/`, with its version pinned in the helmfile.
- Per-environment settings go in `values-<env>.yaml`, never in a chart's own `values.yaml`.
- A new service needs only values: `make new-service` runs `scripts/helm.py add`, which writes `apps/<name>/` and adds it to the peers in `apps/common.yaml`.
- The alert rules, the dashboard, the Postgres bootstrap SQL and the backup script have one copy, under `compose/`. The charts link to it with symlinks.
- An app without `values-<env>.yaml` is not part of that environment (web has no production values yet).

## Install order on a new cluster

Staging and production are separate clusters, each with its own sealing key. Every phase is a set of helmfile releases labelled `phase=<name>`; plain manifest folders (sealed Secrets, issuers, Traefik) are installed as releases too.

| Phase | What |
| --- | --- |
| `bootstrap` | namespaces, quotas and limits; the sealed-secrets controller |
| `secrets` | release `secrets`: the sealed Secrets in `infrastructure/sealed-secrets/secrets/<env>/` |
| `platform` | cert-manager; kube-prometheus-stack (release `monitoring`); `monitoring-extras` |
| `ingress` | releases `cluster-issuers` and `traefik-config` |
| `data` | Postgres and its backup CronJob |
| `apps` | `soroban`, then the other nine services, then web |

```sh
make tools                                         # pinned helmfile and helm-diff, among others
make deploy ENV=staging CONTEXT=<ctx>              # only bootstrap until Secrets are sealed; exits 3
make seal-cert ENV=staging CONTEXT=<ctx>           # certificate (commit) and key backup (keep private)
make seal ENV=staging                              # from .secrets/staging.env; commit infrastructure/sealed-secrets
make deploy ENV=staging CONTEXT=<ctx>              # everything else, in order
```

After that, `make deploy-plan` shows what would change, `make deploy ... ARGS="-l name=<app>"` upgrades one app, and `make deploy-status` shows releases, pods not running and the backups. The full runbook is `docs/deploy.md`.
