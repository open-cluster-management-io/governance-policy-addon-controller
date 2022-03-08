#! /bin/bash

# Number of managed clusters
MANAGED_CLUSTER_COUNT=${MANAGED_CLUSTER_COUNT:-1}
if [[ -n "${MANAGED_CLUSTER_COUNT//[0-9]}" ]] || [[ "${MANAGED_CLUSTER_COUNT}" == "0" ]]; then
  echo "error: provided MANAGED_CLUSTER_COUNT is not a nonzero integer"
  exit 1
fi
KIND_PREFIX=${KIND_PREFIX:-"policy-addon-ctrl"}
CLUSTER_PREFIX=${CLUSTER_PREFIX:-"cluster"}

export KIND_NAME="${KIND_PREFIX}1"
export MANAGED_CLUSTER_NAME="${CLUSTER_PREFIX}1"
# Deploy the hub cluster as cluster1
if [ "${DELETE_CLUSTERS}" == "true" ]; then
  make kind-delete-cluster
else
  make kind-deploy-controller
fi

# Deploy a variable number of managed clusters starting with cluster2
for i in $(seq 2 $((MANAGED_CLUSTER_COUNT+1))); do
  export KIND_NAME="${KIND_PREFIX}${i}"
  export MANAGED_CLUSTER_NAME="${CLUSTER_PREFIX}${i}"
  export HUB_KUBECONFIG="${PWD}/${KIND_PREFIX}1.kubeconfig-internal"
  if [ "${DELETE_CLUSTERS}" == "true" ]; then
    make kind-delete-cluster
  else
    make kind-deploy-registration-operator-managed
    # Approval takes place on the hub
    export KIND_NAME="${KIND_PREFIX}1"
    make kind-approve-cluster
  fi
done
