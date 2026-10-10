# Deploying Shogun with Helm

This is the runbook for deploying Shogun to a Kubernetes cluster (kind first) from released artifacts. Nothing is built on the machine you deploy from: every service image and the Helm chart come from a registry.

## How the pieces fit

| Piece | Where it comes from |
| --- | --- |
| Service image `shogun-<name>:<version>` | Pushed to `ghcr.io/0xhoaxen/shogun-<name>` by the `release` workflow, one per released service |
| Helm chart `shogun-service` | Pushed as an OCI artifact by the `release` workflow after the image push, with the released version |
| Per-service values `deploy/helm/values/<env>/<name>.yaml` | The repo, not the chart (they sit outside `deploy/helm/service`, so `helm package` does not include them). Use a checkout at the version you deploy |
| Postgres 17 + pgvector | `deploy/k8s/postgres` (kustomize) |
| Secrets `shogun-postgres`, `shogun-<name>` | `scripts/k8s-secrets.sh` (the chart never creates them) |
| Web app | `deploy/k8s/web` (kustomize). There is no web chart |

One chart deploys one service. Installing it creates `shogun-<name>` (Deployment, Service, optional HPA, PDB and ServiceMonitor) plus a `shogun-<name>-migrate` Job that runs as a `pre-install,pre-upgrade` hook. Services reach each other at `shogun-<peer>:9090`; the chart sets these as `<PEER>_ADDR`. The Service name comes from `values.name`, not from the Helm release name.

## State of the repo

Checked against `main` (`release.yml`, `deploy.yml`, `deploy/helm/service`):

| Item | State |
| --- | --- |
| Image push to ghcr.io on release | In `release.yml`, but the `push` trigger is commented out and `P3.6` is open, so it runs only through `workflow_dispatch` |
| `helm package` / `helm push` step | **Not in `release.yml` on `main`.** This runbook assumes it exists, as agreed. Until it is merged, use the local chart path instead of the OCI reference (see [Install the services](#install-the-services)) |
| Web image | **Not published.** Build and load it yourself (see [Web app](#web-app)) |
| `imagePullSecrets` in the chart | **Missing.** The Deployment and migration Job templates have no field for it, so a private ghcr.io package cannot be pulled. Make the packages public, or add the field first |
| Production values | `values/production/*.yaml` carry only `ENVIRONMENT` and resources. torii needs `TORII_PUBLIC_URL`, `TORII_PUBLIC_ADDR` and `publicPort.enabled` before a real deploy. They are protected files; the owner edits them |

`TODO(owner)`: record the chart's OCI reference here once the release step exists. Everything below uses `CHART_REF` for it, for example `oci://ghcr.io/0xhoaxen/charts/shogun-service`.

## Before you start

You need `kubectl`, `helm` 3.8 or newer (OCI support), `openssl` 3, and a cluster. For kind:

```sh
kind create cluster --name shogun
export CTX=kind-shogun NS=shogun
export CHART_REF=oci://ghcr.io/0xhoaxen/charts/shogun-service   # TODO(owner): confirm
```

Always pass `--kube-context "$CTX"` (Helm) and `--context "$CTX"` (kubectl). The default context may be a real cluster.

If the chart or images are private, log in first:

```sh
echo "$GHCR_TOKEN" | helm registry login ghcr.io -u <github user> --password-stdin
```

## Pick the versions

Each service is versioned on its own (tags like `services/kagami/v0.4.0`), so there is no single version for the stack. List what is released and write down one version per service:

```sh
gh release list --repo 0xHoaxen/shogun
```

Keep them in a file you can reuse. Do not use `latest`. The tag in `values/<env>/<name>.yaml` is bumped by the deploy workflow's pull requests and is also a valid source:

```sh
# versions.env  (KEY=version)
TORII=1.0.0
KAGAMI=1.0.0
# ... one line per service
```

Check out the repo at a commit whose values files match (the latest merged deploy PR), since the values files are not in the chart.

## 1. Secrets

```sh
cp deploy/k8s/secrets.example.env .secrets/k8s.env   # git-ignored
$EDITOR .secrets/k8s.env                             # Google OAuth, allowed emails, API keys
scripts/k8s-secrets.sh --context "$CTX" --namespace "$NS"
```

The script creates the namespace, `shogun-postgres` and the ten `shogun-<name>` Secrets. It generates database passwords and signing keys and writes them back to `.secrets/k8s.env`. **Keep that file**: Postgres creates its roles once, so passwords must not change, and losing it means the Secrets cannot be recreated. Rerunning changes nothing.

Placeholders for Google sign-in, Gmail, `ANTHROPIC_API_KEY` and `KATANA_GITHUB_*` let the stack start, but sign-in, drafts and GitHub sync will not work until you fill them in.

## 2. Postgres

```sh
kubectl --context "$CTX" kustomize --load-restrictor=LoadRestrictionsNone deploy/k8s/postgres \
  | kubectl --context "$CTX" -n "$NS" apply -f -
kubectl --context "$CTX" -n "$NS" get statefulset            # note the name
kubectl --context "$CTX" -n "$NS" rollout status statefulset/<name> --timeout=180s
```

The init script creates the ten schemas and roles on the first start of an empty volume only. The migration Jobs need them, so wait for the rollout before installing anything. The Secrets script points `DATABASE_URL` at the Service `shogun-postgres`.

## Install the services

Install `soroban` first (every Claude call reserves budget through it), then the rest. Each install runs its migration Job before the Deployment is created, and `--wait` blocks until the pods are Ready.

```sh
. ./versions.env
install_service() {
  name=$1 version=$2
  helm upgrade --install "$name" "$CHART_REF" \
    --version "$version" \
    --kube-context "$CTX" --namespace "$NS" \
    -f "deploy/helm/values/staging/$name.yaml" \
    --set image.tag="$version" \
    --wait --timeout 5m
}

install_service soroban "$SOROBAN"
for s in kagami fude tsubame taiko dojo katana shinobi sensei torii; do
  v=$(printf '%s' "$s" | tr '[:lower:]' '[:upper:]')
  install_service "$s" "${!v}"
done
```

(`${!v}` needs bash; zsh users run it as `bash -c` or use `${(P)v}`.)

Notes:

- `--version` selects the chart from the registry; `image.tag` selects the image. They are set separately on purpose. If the release step publishes the chart with the service version, pass the same value to both. If it publishes one chart version for all services, use that for `--version` and each service's own version for `image.tag`. `TODO(owner)`: state which in [State of the repo](#state-of-the-repo) once decided.
- Without the release step on `main`, replace `"$CHART_REF" --version "$version"` with `deploy/helm/service` from a local checkout. The result is the same chart at the checked-out commit.
- Every pod gets all ten `*_ADDR` values, so the install order does not matter to Helm. `--wait` can still time out if a service refuses to become Ready until a peer is up; if it does, install the others and rerun.
- A failed migration Job fails the install. Read it with `kubectl -n "$NS" logs job/shogun-<name>-migrate`. Helm deletes the Job on success, and `before-hook-creation` removes a failed one on the next try.
- Use `values/production/` instead of `values/staging/` only after the production gaps listed above are closed.

## Web app

The Next.js rewrites to torii are baked in at build time, so the image must be built with torii's in-cluster address:

```sh
docker build -f deploy/docker/web.Dockerfile \
  --build-arg TORII_URL=http://shogun-torii:8081 \
  -t shogun-web:<tag> .
kind load docker-image shogun-web:<tag> --name shogun
(cd deploy/k8s/web && kustomize edit set image shogun-web=shogun-web:<tag>)   # do not commit this change
kubectl --context "$CTX" -n "$NS" apply -k deploy/k8s/web
kubectl --context "$CTX" -n "$NS" rollout status deployment/shogun-web
```

Use a unique `<tag>` per build, not `local`, or the pod will not pick up a new image. On a real cluster push the image to a registry instead of `kind load`. Check the image name in `deploy/k8s/web/kustomization.yaml` before running `kustomize edit`.

## Verify

```sh
kubectl --context "$CTX" -n "$NS" get pods                 # all Running/Ready, no restarts
kubectl --context "$CTX" -n "$NS" get jobs                 # no failed migrate Jobs left behind
helm --kube-context "$CTX" -n "$NS" list                   # ten releases, status deployed
kubectl --context "$CTX" -n "$NS" port-forward svc/shogun-web 3000:3000
```

Then, with the port-forward running:

1. `curl -fsSI http://localhost:3000/login | head -1` returns 200.
2. Open http://localhost:3000 and sign in with an address in `TORII_ALLOWED_EMAILS`. The redirect URI registered in Google must be `http://localhost:3000/auth/callback`, so keep `TORII_PUBLIC_URL` and the port-forward port the same.
3. Check one service's readiness: `kubectl -n "$NS" port-forward svc/shogun-kagami 8080:8080` and `curl -fsS localhost:8080/readyz`.
4. Add a draft, approve it and send it to yourself, once, to exercise the whole Hanko path.

The smoke test script from `P10.5` is not written yet; the steps above are what it should automate.

## Upgrade

A service releases on its own, so an upgrade is one `helm upgrade` per changed service:

```sh
install_service kagami 1.1.0
```

The migration Job runs first (`pre-upgrade`), then the pods roll. Migrations are append-only and there is no down step. Roll back a bad release with `helm rollback`, but only if the new version added no migration; if it did, the old code does not know the new schema and you restore a database backup instead.

```sh
helm --kube-context "$CTX" -n "$NS" history kagami
helm --kube-context "$CTX" -n "$NS" rollback kagami <revision> --wait
```

Take a database dump before any upgrade that adds a migration. There is no backup job in the cluster (the Compose `backup` service does not exist here), so run it by hand; the superuser name and password come from the `shogun-postgres` Secret and the StatefulSet env, so check them first:

```sh
kubectl --context "$CTX" -n "$NS" exec statefulset/<name> -- \
  sh -c 'pg_dump -U "$POSTGRES_USER" -Fc shogun' > "shogun-$(date +%F).dump"
```

## Remove

```sh
for s in torii sensei shinobi katana dojo taiko tsubame fude kagami soroban; do
  helm --kube-context "$CTX" -n "$NS" uninstall "$s"
done
kubectl --context "$CTX" delete namespace "$NS"      # also deletes Postgres data and the Secrets
```

`kind delete cluster --name shogun` removes everything.

## Security checklist before a real login

- [ ] `.secrets/k8s.env` is mode 600, not in git, and backed up.
- [ ] `ENVIRONMENT=production` and `TORII_PUBLIC_URL` is `https://` in the production values.
- [ ] No placeholder remains: `grep replace-me .secrets/k8s.env` prints nothing.
- [ ] `TORII_ALLOWED_EMAILS` lists only you.
- [ ] Only the web app is exposed outside the cluster; Postgres and the service Services stay `ClusterIP`.
- [ ] Hanko keys match (a test send succeeds; a tampered draft is refused).
- [ ] The Google OAuth consent screen is limited to you.
- [ ] A database dump exists off the cluster and one restore has been checked.

## Open items

| Item | Owner |
| --- | --- |
| Chart OCI reference and version scheme (one chart version, or the service version) | `TODO(owner)` |
| `helm package` and `helm push` step in `release.yml`, and enabling its `push` trigger (`P3.6`) | task to add |
| `imagePullSecrets` in the chart, if the ghcr.io packages stay private | task to add |
| Publish the web image from CI | task to add |
| Ingress with TLS for the web app (the port-forward is for kind only) | `TODO(owner)` |
| Backup CronJob and off-cluster destination | `TODO(owner)` |
| Production values: torii URL, public port and peers | `TODO(owner)` (protected files) |
