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
if [[ -n "${MANAGED_CLUSTER_COUNT//[0-9]/}" ]] || [[ "${MANAGED_CLUSTER_COUNT}" == "0" ]]; then
  echo "error: provided MANAGED_CLUSTER_COUNT is not a nonzero integer"
  exit 1
fi

KIND_PREFIX=${KIND_PREFIX:-"policy-addon-ctrl"}
CLUSTER_PREFIX=${CLUSTER_PREFIX:-"cluster"}
KUBECONFIG_HUB="${PWD}/kubeconfig_${CLUSTER_PREFIX}1_e2e"

export KIND_NAME="${KIND_PREFIX}1"
export CLUSTER_NAME="${CLUSTER_PREFIX}1"
# Deploy the hub cluster as cluster1
case ${RUN_MODE} in
delete)
  make kind-delete-cluster
  ;;
debug)
  make e2e-debug
  ;;
create)
  make kind-deploy-controller
  ;;
create-dev)
  make kind-prep-ocm
  ;;
deploy-addons)
  make kind-deploy-addons
  ;;
esac

if [[ "${RUN_MODE}" == "create" || "${RUN_MODE}" == "create-dev" ]]; then
  echo Annotating the ManagedCluster object to indicate it is a hub
  KUBECONFIG=${KUBECONFIG_HUB} kubectl annotate ManagedCluster $CLUSTER_NAME --overwrite "addon.open-cluster-management.io/on-multicluster-hub=true"

  echo Generating the service account kubeconfig
  KIND_KUBECONFIG=${KUBECONFIG_HUB} make kind-controller-kubeconfig
fi

# Deploy a variable number of managed clusters starting with cluster2
for i in $(seq 2 $((MANAGED_CLUSTER_COUNT + 1))); do
  export KIND_NAME="${KIND_PREFIX}${i}"
  export CLUSTER_NAME="${CLUSTER_PREFIX}${i}"
  export KLUSTERLET_NAME="${CLUSTER_NAME}-klusterlet"

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
    KIND_KUBECONFIG=${KUBECONFIG_HUB} make kind-approve-cluster

    # Deploy "name" ClusterClaim to label ManagedCluster
    cat <<EOF | kubectl apply --kubeconfig="${PWD}/kubeconfig_${CLUSTER_NAME}_e2e" -f -
apiVersion: cluster.open-cluster-management.io/v1alpha1
kind: ClusterClaim
metadata:
  name: name
spec:
  value: ${CLUSTER_NAME}
EOF

    # Temporary ManagedCluster label workaround since the ClusterClaim controller
    # isn't running in our current setup
    kubectl --kubeconfig="${KUBECONFIG_HUB}" label managedcluster ${CLUSTER_NAME} name=${CLUSTER_NAME}
    ;;

  deploy-addons)
    # ManagedClusterAddon is applied to the hub
    KIND_KUBECONFIG=${KUBECONFIG_HUB} make kind-deploy-addons
    ;;
  esac
done
