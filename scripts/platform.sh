#!/usr/bin/env bash
# Installs or upgrades the cluster-wide pieces the app releases rely on: cert-manager,
# the ingress chart (Traefik's HTTP to HTTPS redirect and the Let's Encrypt
# issuers), kube-prometheus-stack, then the monitoring chart (Shogun's alert rules
# and dashboard). Run it once per cluster before scripts/deploy.sh, whose
# ServiceMonitors need the Prometheus Operator CRDs, and again after changing a
# version here or a file in deploy/helm/values/cluster/.
#
# Usage: scripts/platform.sh --context <ctx>
#
# Run scripts/k8s-secrets.sh first: Grafana reads its login from Secret
# grafana-admin. Written for k3s, which bundles Traefik. Upstream charts are
# pinned below; the local charts are installed from the checkout.
set -euo pipefail

HELM="${HELM:-helm}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CERT_MANAGER_CHART="oci://quay.io/jetstack/charts/cert-manager"
CERT_MANAGER_VERSION="v1.21.2"
CERT_MANAGER_NAMESPACE="cert-manager"
# The HelmChartConfig must live next to k3s's Traefik HelmChart.
INGRESS_NAMESPACE="kube-system"
KPS_CHART="oci://ghcr.io/prometheus-community/charts/kube-prometheus-stack"
KPS_VERSION="92.3.0"
MONITORING_NAMESPACE="monitoring"
TIMEOUT="5m"
# kube-prometheus-stack pulls several images on a fresh node.
KPS_TIMEOUT="10m"
CLUSTER_VALUES="kube-prometheus-stack"

die() {
	echo "platform: $*" >&2
	exit 2
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

# install_kps CONTEXT VALUES_DIR installs Prometheus, Alertmanager and Grafana.
install_kps() {
	local context=$1 values_dir=$2
	"$HELM" upgrade --install kube-prometheus-stack "$KPS_CHART" \
		--version "$KPS_VERSION" \
		--kube-context "$context" --namespace "$MONITORING_NAMESPACE" --create-namespace \
		-f "$values_dir/kube-prometheus-stack.yaml" \
		--wait --timeout "$KPS_TIMEOUT"
	echo "installed kube-prometheus-stack $KPS_VERSION"
}

# install_monitoring CONTEXT installs the local chart with Shogun's rules and dashboard.
install_monitoring() {
	local context=$1
	"$HELM" upgrade --install monitoring "$ROOT/deploy/helm/monitoring" \
		--kube-context "$context" --namespace "$MONITORING_NAMESPACE" \
		--wait --timeout "$TIMEOUT"
	echo "installed monitoring (local chart)"
}

main() {
	local context=""
	while [ $# -gt 0 ]; do
		case $1 in
		--context) context=${2:-}; shift 2 ;;
		*) die "unknown argument: $1" ;;
		esac
	done
	[ -n "$context" ] || die "--context is required (never installs to the current context by default)"

	local values_dir="$ROOT/deploy/helm/values/cluster" name
	for name in $CLUSTER_VALUES; do
		[ -f "$values_dir/$name.yaml" ] || die "missing $values_dir/$name.yaml"
	done

	install_cert_manager "$context"
	install_ingress "$context"
	install_kps "$context" "$values_dir"
	install_monitoring "$context"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	main "$@"
fi
