#! /bin/bash

set -e
set -o pipefail

if [[ "${HOSTED_MODE}" == "true" ]]; then
  RUN_MODE=${RUN_MODE:-"create"}
else
  RUN_MODE=${RUN_MODE:-"create-dev"}
fi

# Number of managed clusters
MANAGED_CLUSTER_COUNT="${MANAGED_CLUSTER_COUNT:-1}"
if [[ -n "${MANAGED_CLUSTER_COUNT//[0-9]}" ]] || [[ "${MANAGED_CLUSTER_COUNT}" == "0" ]]; then
  echo "error: provided MANAGED_CLUSTER_COUNT is not a nonzero integer"
  exit 1
fi

HUB_KIND_VERSION=$KIND_VERSION
if [[ "${KIND_VERSION}" == "minimum" ]]; then
  # The hub supports fewer Kubernetes versions than the managed cluster.
  HUB_KIND_VERSION=v1.23.13
fi

KIND_PREFIX=${KIND_PREFIX:-"policy-addon-ctrl"}
CLUSTER_PREFIX=${CLUSTER_PREFIX:-"cluster"}

export KIND_NAME="${KIND_PREFIX}1"
export MANAGED_CLUSTER_NAME="${CLUSTER_PREFIX}1"
# Deploy the hub cluster as cluster1
case ${RUN_MODE} in
  delete)
    make kind-delete-cluster
    ;;
  debug)
    make e2e-debug
    ;;
  create)
    KIND_VERSION="${HUB_KIND_VERSION}" make kind-deploy-controller
    ;;
  create-dev)
    KIND_VERSION="${HUB_KIND_VERSION}" make kind-prep-ocm
    ;;
  deploy-addons)
    make kind-deploy-addons
    ;;
esac

if [[ "${RUN_MODE}" == "create" || "${RUN_MODE}" == "create-dev" ]]; then
  echo Annotating the ManagedCluster object to indicate it is a hub
  KUBECONFIG="$PWD/$KIND_NAME.kubeconfig" kubectl annotate ManagedCluster $MANAGED_CLUSTER_NAME --overwrite "addon.open-cluster-management.io/on-multicluster-hub=true"
fi

# Deploy a variable number of managed clusters starting with cluster2
for i in $(seq 2 $((MANAGED_CLUSTER_COUNT+1))); do
  export KIND_NAME="${KIND_PREFIX}${i}"
  export MANAGED_CLUSTER_NAME="${CLUSTER_PREFIX}${i}"
  export KLUSTERLET_NAME="${MANAGED_CLUSTER_NAME}-klusterlet"
  case ${RUN_MODE} in
    delete)
      make kind-delete-cluster
      ;;
    debug)
      make e2e-debug
      ;;
    create | create-dev)
      if [[ "${HOSTED_MODE}" == "true" ]]; then
        make kind-deploy-registration-operator-managed-hosted
      else
        make kind-deploy-registration-operator-managed
      fi

      # Approval takes place on the hub
      export KIND_NAME="${KIND_PREFIX}1"
      make kind-approve-cluster
      ;;
    deploy-addons)
      # ManagedClusterAddon is applied to the hub
      export KIND_NAME="${KIND_PREFIX}1"
      make kind-deploy-addons
      ;;
  esac
done
