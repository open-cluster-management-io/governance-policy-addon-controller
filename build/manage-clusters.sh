#! /bin/bash

if [[ "${HOSTED_MODE}" == "true" ]]; then
  RUN_MODE=${RUN_MODE:-"create"}
else
  RUN_MODE=${RUN_MODE:-"create-dev"}
fi

# Number of managed clusters
MANAGED_CLUSTER_COUNT=${MANAGED_CLUSTER_COUNT:-1}
if [[ -n "${MANAGED_CLUSTER_COUNT//[0-9]}" ]] || [[ "${MANAGED_CLUSTER_COUNT}" == "0" ]]; then
  echo "error: provided MANAGED_CLUSTER_COUNT is not a nonzero integer"
  exit 1
fi

if [[ "${MANAGED_CLUSTER_COUNT}" -gt 1 ]] && [[ "${HOSTED_MODE}" == "true" ]]; then
  # This is a current limitation in the registration operator Makefile where the hosted mode Klusterlet object name
  # is not customizable.
  echo "error: provided MANAGED_CLUSTER_COUNT cannot be greater than 1 in hosted mode"
  exit 1
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
    make kind-deploy-controller
    ;;
  create-dev)
    make kind-prep-ocm
    ;;
  deploy-addons)
    make kind-deploy-addons-hub
    ;;
esac

# Deploy a variable number of managed clusters starting with cluster2
for i in $(seq 2 $((MANAGED_CLUSTER_COUNT+1))); do
  export KIND_NAME="${KIND_PREFIX}${i}"
  export MANAGED_CLUSTER_NAME="${CLUSTER_PREFIX}${i}"
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
      make kind-deploy-addons-managed
      ;;
  esac
done
