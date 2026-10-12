# Deploying Shogun to a cluster

This is the runbook for deploying Shogun to a Kubernetes cluster (k3s on a VM, or kind to try it) from released images. Nothing is built on the machine you deploy from: images come from ghcr.io; the charts and values come from the checkout. Layout: `deploy/README.md`.

## How the pieces fit

| Piece | Where it comes from |
| --- | --- |
| Images `ghcr.io/0xhoaxen/<name>:<version>` | The `release` workflow, one per released service, and `web` |
| One chart for every app | `deploy/charts/microservice`, from the checkout (not published) |
| App values | `deploy/apps/<name>/values.yaml` and `values-<env>.yaml`; `image.tag` is bumped by `deploy.yml` pull requests |
| Postgres 17 + pgvector, nightly backup | `deploy/charts/postgres`, values in `deploy/infrastructure/postgres/` |
| Everything else in the cluster | upstream charts with values in `deploy/infrastructure/<component>/`, versions pinned in `helmfile.yaml.gotmpl` |
| Secrets `postgres`, `<name>`, `grafana-admin`, `ghcr` | sealed with `make seal` (`scripts/seal-secrets.sh`), committed in `deploy/infrastructure/sealed-secrets/secrets/<env>/`; no chart creates them |

Each app release creates `<name>` (Deployment, Service, optional HPA, PDB and ServiceMonitor) plus, for the Go services, a `<name>-migrate` Job that runs as a `pre-install,pre-upgrade` hook. Services reach each other at `<peer>:9090` (`<PEER>_ADDR`). The web app is the only thing exposed, through Traefik.

## Before you start

You need `kubectl`, `helm` 4, `kubeseal`, `openssl` 3 and a cluster. `make tools` installs the pinned `helmfile` into `./bin` and the `helm-diff` plugin.

```sh
kind create cluster --name shogun      # or use the k3s VM's context
export CTX=kind-shogun
```

Every `make deploy*` target needs `ENV` and `CONTEXT` and refuses without them; the current kubectl context is never used on its own, because that may be a real cluster. `ARGS` passes extra flags to `helmfile`, for example a selector (`ARGS="-l name=kagami"`) or `--debug`. Everything runs in the foreground, so helmfile's own output is the log.

If the images are private, `GHCR_USER` and a `GHCR_TOKEN` with `read:packages` go in the env file (section 1), which seals the pull secret `ghcr` that `deploy/apps/common.yaml` names.

## 0. First install

```sh
make deploy ENV=staging CONTEXT="$CTX"     # helmfile apply --interactive --concurrency 1
```

It shows the diff and asks before applying (production also asks you to type the context name first; `YES=1` skips both, for CI). Releases install one at a time, in the order their `needs` give. The phases, in order:

1. `bootstrap`: namespaces `shogun` and `monitoring`, a LimitRange (50m CPU and 64Mi request, 256Mi memory limit per container by default) and a ResourceQuota on `shogun` sized for a 2 vCPU / 4 GiB VM; the sealed-secrets controller. Production does not fit that quota yet: `scripts/k8s_test.sh` prints how far over it is.
2. `secrets`: release `secrets`, the sealed files in `deploy/infrastructure/sealed-secrets/secrets/<env>/`. On a new cluster none are committed yet, so the helmfile declares only `bootstrap`; `make deploy` installs that, prints the next commands and exits 3. Seal (section 1), commit and run `make deploy` again.
3. `platform`: cert-manager with its CRDs; kube-prometheus-stack as release `monitoring` (Prometheus 7 days on 5Gi `local-path`, Alertmanager, Grafana); `monitoring-extras` with Shogun's six alert rules and the "Shogun overview" dashboard, linked from `deploy/compose/observability/` so Compose and the cluster share one copy.
4. `ingress`: release `cluster-issuers` (`letsencrypt-staging` and `letsencrypt-prod`) and release `traefik-config`, the k3s Traefik redirect from HTTP to HTTPS (needs Traefik chart 34 or newer: `kubectl -n kube-system get helmchart traefik -o jsonpath='{.spec.chart}'`). On a cluster that is not k3s (kind) the HelmChartConfig kind does not exist, so leave that release out: `ARGS="-l name!=traefik-config"`.
5. `data`: Postgres and its nightly backup CronJob. The init script creates the ten schemas and roles on the first start of an empty volume only; the migration Jobs need them.
6. `apps`: `soroban` first (every Claude call reserves budget through it), then the other nine services, then web. Each Go service runs its migration Job before its pods start.

A rerun is safe: releases that did not change are skipped. To run one phase, `ARGS="-l phase=<phase>"`. Settings live in `deploy/infrastructure/<component>/values*.yaml`; rerun `make deploy` after changing one.

The sealed Secrets, issuers and Traefik config were applied with `kubectl` before they became releases. On such a cluster, run the first `make deploy` with `ARGS="--state-values-set adopt=true"` so their releases adopt them instead of failing. Leave it off on a new cluster: taking ownership makes the diff look up the ClusterIssuer and SealedSecret kinds before their CRDs are installed.

## 1. Secrets

Secrets are sealed with kubeseal and committed, so the cluster can be rebuilt from git. Sealing needs only the controller's public certificate, so it works offline.

```sh
cp deploy/infrastructure/sealed-secrets/secrets.example.env .secrets/staging.env   # git-ignored
$EDITOR .secrets/staging.env                                                       # Google OAuth, allowed emails, API keys
make seal-cert ENV=staging CONTEXT="$CTX"   # certs/staging-pub-cert.pem (public, commit it)
                                             # and .secrets/staging-sealed-secrets-key.yaml (private)
make seal ENV=staging                        # secrets/staging/*.yaml
git add deploy/infrastructure/sealed-secrets && git commit
make deploy ENV=staging CONTEXT="$CTX"
```

It seals `postgres` and the ten `<name>` Secrets for `shogun`, `grafana-admin` for `monitoring`, and `ghcr` when `GHCR_USER` and `GHCR_TOKEN` are set. It generates database passwords and signing keys and writes them back to `.secrets/staging.env`; values are never printed or logged. **Keep that file**: it is the plaintext source, and Postgres creates its roles once, so a regenerated password would not be accepted. **Back up the key file too, outside git**: a rebuilt cluster gets a new sealing key and cannot decrypt what is committed without the old one. Restore it into `kube-system` before the controller starts, then restart the controller. To change one value, edit the env file and run `make seal ENV=staging ARGS="--only <name>"`, then `make deploy ENV=staging CONTEXT="$CTX" ARGS="-l name=secrets"`. Older runs wrote a `kustomization.yaml` into the sealed folder; `make seal` removes it, because helmfile would need `kustomize` to read the folder with it.

Each environment is its own cluster with its own key and certificate, so seal staging and production separately. A SealedSecret is bound to its name and namespace (strict scope).

Placeholders for Google sign-in, Gmail, `ANTHROPIC_API_KEY` and `KATANA_GITHUB_*` let the stack start, but sign-in, drafts and GitHub sync will not work until you fill them in; `seal` warns about each one.

## 2. Exposing the web app and Grafana

Point a DNS record for the host at the VM (ports 80 and 443 open), then set `ingress.enabled: true` and `ingress.host` in `deploy/apps/web/values-<env>.yaml` (see the commented block in the staging file) and run `make deploy ENV=<env> CONTEXT="$CTX" ARGS="-l name=web"`. Start with `clusterIssuer: letsencrypt-staging`; once `kubectl -n shogun get certificate web-tls` is Ready, switch to `letsencrypt-prod`. Set `TORII_PUBLIC_URL`, `TSUBAME_GMAIL_REDIRECT_URL` and the Google redirect URIs to `https://<host>`, re-seal with `ARGS="--only torii --only tsubame"`, commit, and run `make deploy`. Traefik's access log stays off: it would record the OAuth callback's code.

Grafana is reached through Traefik once its host is set (the `TODO(owner)` block under `grafana:` in `deploy/infrastructure/monitoring/kube-prometheus-stack/values.yaml`). Until then, port-forward. Sign in as `admin` with `GRAFANA_ADMIN_PASSWORD` from `.secrets/<env>.env`. Prometheus and Alertmanager are never exposed:

```sh
kubectl --context "$CTX" -n monitoring port-forward svc/monitoring-grafana 3001:80
kubectl --context "$CTX" -n monitoring port-forward svc/monitoring-kube-prometheus-prometheus 9190:9090   # Status > Targets: ten services up
kubectl --context "$CTX" -n monitoring port-forward svc/monitoring-kube-prometheus-alertmanager 9191:9093
```

Alertmanager sends to the chart's `null` receiver until an email receiver is added (`TODO(owner)` in the same values file).

## 3. Verify

```sh
make deploy-status ENV=staging CONTEXT="$CTX"     # releases, pods not running, backup jobs
kubectl --context "$CTX" -n shogun port-forward svc/web 3000:3000
```

Then, with the port-forward running:

1. `curl -fsSI http://localhost:3000/login | head -1` returns 200.
2. Open http://localhost:3000 and sign in with an address in `TORII_ALLOWED_EMAILS`. The redirect URI registered in Google must be `http://localhost:3000/auth/callback`, so keep `TORII_PUBLIC_URL` and the port-forward port the same.
3. Check one service's readiness: `kubectl -n shogun port-forward svc/kagami 8080:8080` and `curl -fsS localhost:8080/readyz`.
4. Add a draft, approve it and send it to yourself, once, to exercise the whole Hanko path.
5. `make backup-now ENV=staging CONTEXT="$CTX"` writes a backup; restore it once (`docs/backup-restore.md`, "On the cluster").

The smoke test script from `P10.5` is not written yet; the steps above are what it should automate.

## 4. Upgrade

A service releases on its own: the release workflow opens a pull request that bumps `image.tag` in `deploy/apps/<name>/values-staging.yaml`. After merging it:

```sh
make deploy-plan ENV=staging CONTEXT="$CTX"                         # helmfile diff
make deploy ENV=staging CONTEXT="$CTX" ARGS="-l name=kagami"        # just that app
make deploy ENV=staging CONTEXT="$CTX" \
  ARGS="-l name=kagami --state-values-set tags.kagami=1.1.0"        # try a version without editing values
```

The migration Job runs first (`pre-upgrade`), then the pods roll. Migrations are append-only and there is no down step. Roll back a bad release with `helm rollback`, but only if the new version added no migration; if it did, the old code does not know the new schema and you restore a backup instead. Take one first with `make backup-now`.

```sh
helm --kube-context "$CTX" -n shogun history kagami
helm --kube-context "$CTX" -n shogun rollback kagami <revision> --wait
```

## 5. Remove

```sh
bin/helmfile -e staging --kube-context "$CTX" destroy   # every release; namespaces and the backup volume are kept
kubectl --context "$CTX" delete namespace shogun        # also deletes Postgres data, backups and the Secrets
```

`kind delete cluster --name shogun` removes everything.

## Security checklist before a real login

- [ ] `.secrets/<env>.env` is mode 600, not in git, and backed up; so is the sealing key backup.
- [ ] `ENVIRONMENT=production` and `TORII_PUBLIC_URL` is `https://` in the production values.
- [ ] No placeholder remains: `grep replace-me .secrets/<env>.env` prints nothing.
- [ ] `TORII_ALLOWED_EMAILS` lists only you.
- [ ] Only the web app and Grafana are exposed outside the cluster; Postgres, Prometheus, Alertmanager and the service Services stay `ClusterIP`.
- [ ] Hanko keys match (a test send succeeds; a tampered draft is refused).
- [ ] The Google OAuth consent screen is limited to you.
- [ ] A backup exists off the cluster and one restore has been checked.

## Open items

| Item | Owner |
| --- | --- |
| Enable the `push` trigger of `release.yml` (`P10.5k`) | task |
| Web host and DNS record for the Ingress (`ingress.host` in `deploy/apps/web/values-<env>.yaml`) | `TODO(owner)` |
| Production web values (`deploy/apps/web/values-production.yaml`) | `TODO(owner)` |
| Production quota: the HPA minimum of two replicas does not fit a 4 GiB VM | `TODO(owner)` |
| Backup retention and an off-cluster copy | `TODO(owner)` |
| Production values: torii URL and public port | `TODO(owner)` (protected files) |
