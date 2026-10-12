#!/usr/bin/env bash
# Installs or upgrades the cluster-wide pieces the app releases rely on: the
# namespaces and quotas in deploy/k8s, the sealed-secrets controller and the
# sealed Secrets, cert-manager,
# the ingress chart (Traefik's HTTP to HTTPS redirect and the Let's Encrypt
# issuers), then the monitoring chart (Prometheus, Alertmanager and Grafana with
# Shogun's alert rules and dashboard). Run it once per cluster before
# scripts/deploy.sh, whose ServiceMonitors need the Prometheus Operator CRDs, and
# again after changing a version here or in deploy/helm/monitoring.
#
# Usage: scripts/platform.sh --context <ctx> --env <staging|production>
#
# The first run on a new cluster stops after the controller (exit 3): seal the
# Secrets with scripts/seal-secrets.sh, commit deploy/k8s/<env>, run it again.
# Grafana reads its login from the sealed Secret grafana-admin. Written for k3s, which bundles Traefik. cert-manager is pinned
# below, the Prometheus stack in deploy/helm/monitoring/Chart.yaml; the local
# charts are installed from the checkout.
set -euo pipefail

HELM="${HELM:-helm}"
KUBECTL="${KUBECTL:-kubectl}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CERT_MANAGER_CHART="oci://quay.io/jetstack/charts/cert-manager"
CERT_MANAGER_VERSION="v1.21.2"
CERT_MANAGER_NAMESPACE="cert-manager"
SEALED_SECRETS_REPO="https://bitnami.github.io/sealed-secrets"
SEALED_SECRETS_VERSION="2.20.0"
# kubeseal looks for exactly this name and namespace unless told otherwise.
SEALED_SECRETS_NAME="sealed-secrets-controller"
SEALED_SECRETS_NAMESPACE="kube-system"
EXIT_NOT_SEALED=3
# The HelmChartConfig must live next to k3s's Traefik HelmChart.
INGRESS_NAMESPACE="kube-system"
MONITORING_NAMESPACE="monitoring"
TIMEOUT="5m"
# The Prometheus stack pulls several images on a fresh node.
MONITORING_TIMEOUT="10m"

die() {
	echo "platform: $*" >&2
	exit 2
}

# apply_cluster_basics CONTEXT ENV creates the namespaces, LimitRange and
# ResourceQuota from deploy/k8s/<env>.
apply_cluster_basics() {
	local context=$1 env=$2
	"$KUBECTL" --context "$context" apply -k "$ROOT/deploy/k8s/$env" >/dev/null
	echo "applied deploy/k8s/$env"
}

# install_sealed_secrets CONTEXT installs the controller that decrypts SealedSecrets.
install_sealed_secrets() {
	local context=$1
	"$HELM" upgrade --install sealed-secrets sealed-secrets \
		--repo "$SEALED_SECRETS_REPO" --version "$SEALED_SECRETS_VERSION" \
		--kube-context "$context" --namespace "$SEALED_SECRETS_NAMESPACE" \
		--set-string "fullnameOverride=$SEALED_SECRETS_NAME" \
		--wait --timeout "$TIMEOUT"
	echo "installed sealed-secrets $SEALED_SECRETS_VERSION"
}

# apply_secrets CONTEXT ENV applies the sealed Secrets, or stops with the steps to
# create them when none are committed yet.
apply_secrets() {
	local context=$1 env=$2 dir="$ROOT/deploy/k8s/$2/secrets"
	if [ ! -f "$dir/kustomization.yaml" ]; then
		cat >&2 <<EOF
platform: no sealed Secrets in deploy/k8s/$env/secrets yet. The controller is installed; now:
  scripts/seal-secrets.sh --env $env --fetch-cert --context $context
  scripts/seal-secrets.sh --env $env --backup-key --context $context
  scripts/seal-secrets.sh --env $env
  git add deploy/k8s/$env && git commit, then run this script again.
EOF
		exit "$EXIT_NOT_SEALED"
	fi
	"$KUBECTL" --context "$context" apply -k "$dir" >/dev/null
	echo "applied deploy/k8s/$env/secrets"
}

# install_cert_manager CONTEXT installs cert-manager with its CRDs and waits for
# its webhook, which must answer before any ClusterIssuer can be created.
install_cert_manager() {
	local context=$1
	"$HELM" upgrade --install cert-manager "$CERT_MANAGER_CHART" \
		--version "$CERT_MANAGER_VERSION" \
		--kube-context "$context" --namespace "$CERT_MANAGER_NAMESPACE" --create-namespace \
		--set crds.enabled=true \
		--wait --timeout "$TIMEOUT"
	echo "installed cert-manager $CERT_MANAGER_VERSION"
}

# install_ingress CONTEXT installs the local ingress chart.
install_ingress() {
	local context=$1
	"$HELM" upgrade --install ingress "$ROOT/deploy/helm/platform/ingress" \
		--kube-context "$context" --namespace "$INGRESS_NAMESPACE" \
		--wait --timeout "$TIMEOUT"
	echo "installed ingress (local chart)"
}

# install_monitoring CONTEXT installs the local umbrella chart: the Prometheus
# stack plus Shogun's rules and dashboard, in one release.
install_monitoring() {
	local context=$1
	"$HELM" upgrade --install monitoring "$ROOT/deploy/helm/monitoring" \
		--dependency-update \
		--kube-context "$context" --namespace "$MONITORING_NAMESPACE" \
		--wait --timeout "$MONITORING_TIMEOUT"
	echo "installed monitoring (local chart)"
}

main() {
	local context="" env=""
	while [ $# -gt 0 ]; do
		case $1 in
		--context) context=${2:-}; shift 2 ;;
		--env) env=${2:-}; shift 2 ;;
		*) die "unknown argument: $1" ;;
		esac
	done
	[ -n "$context" ] || die "--context is required (never installs to the current context by default)"
	case $env in
	staging | production) ;;
	*) die "--env must be staging or production" ;;
	esac
	[ -d "$ROOT/deploy/k8s/$env" ] || die "missing $ROOT/deploy/k8s/$env"

	apply_cluster_basics "$context" "$env"
	install_sealed_secrets "$context"
	apply_secrets "$context" "$env"
	install_cert_manager "$context"
	install_ingress "$context"
	install_monitoring "$context"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	main "$@"
fi
