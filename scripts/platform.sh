#!/usr/bin/env bash
# Installs or upgrades the cluster-wide pieces the app releases rely on: cert-manager,
# then the ingress chart (Traefik's HTTP to HTTPS redirect and the Let's Encrypt
# issuers). Run it once per cluster before scripts/deploy.sh, and again after
# changing a version here or a file in deploy/helm/values/cluster/.
#
# Usage: scripts/platform.sh --context <ctx>
#
# Written for k3s, which bundles Traefik. Upstream charts are pinned below; the
# local ingress chart is installed from the checkout.
set -euo pipefail

HELM="${HELM:-helm}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CERT_MANAGER_CHART="oci://quay.io/jetstack/charts/cert-manager"
CERT_MANAGER_VERSION="v1.21.2"
CERT_MANAGER_NAMESPACE="cert-manager"
# The HelmChartConfig must live next to k3s's Traefik HelmChart.
INGRESS_NAMESPACE="kube-system"
TIMEOUT="5m"

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

# install_ingress CONTEXT VALUES_DIR installs the local ingress chart.
install_ingress() {
	local context=$1 values_dir=$2
	"$HELM" upgrade --install ingress "$ROOT/deploy/helm/ingress" \
		--kube-context "$context" --namespace "$INGRESS_NAMESPACE" \
		-f "$values_dir/ingress.yaml" \
		--wait --timeout "$TIMEOUT"
	echo "installed ingress (local chart)"
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

	local values_dir="$ROOT/deploy/helm/values/cluster"
	[ -f "$values_dir/ingress.yaml" ] || die "missing $values_dir/ingress.yaml"

	install_cert_manager "$context"
	install_ingress "$context" "$values_dir"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	main "$@"
fi
