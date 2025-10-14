# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

The Governance Policy Addon Controller manages installations of policy addons on managed clusters in Open Cluster Management (OCM) using ManifestWorks. It manages three addons:
- `config-policy-controller` - Configuration Policy Controller
- `governance-policy-framework` - Governance Policy Framework Addon
- `governance-standalone-hub-templating` - Standalone Hub Templating

The controller uses the OCM addon-framework to deploy and configure these addons via `ManagedClusterAddOn` resources.

## Build, Test, and Development Commands

### Building
```bash
# Build the controller binary
make build

# Build container image
make build-images
```

### Testing
```bash
# Run unit tests
make test

# Run unit tests with coverage
make test-coverage

# Run e2e tests (requires KinD cluster setup)
make e2e-test

# Run e2e tests for hosted mode
make e2e-test-hosted-mode

# Run e2e tests with coverage using instrumented controller
make e2e-test-coverage
```

### Local Development with KinD

```bash
# Bootstrap KinD cluster with everything deployed
make kind-bootstrap-cluster

# Bootstrap KinD cluster for local development (controller runs locally)
make kind-bootstrap-cluster-dev

# Wait for work agent to be ready (required before deploying addons)
make wait-for-work-agent

# Deploy ManagedClusterAddon resources to deploy governance addons
make kind-deploy-addons

# Run controller locally against KinD cluster
make kind-run-local

# Delete KinD clusters
make kind-bootstrap-delete-clusters
```

### Updating a Running Controller

```bash
# Build and load new image into KinD
make kind-load-image

# Update deployment manifests and restart controller
make kind-regenerate-controller
```

### Code Generation
```bash
# Generate manifests (RBAC, CRDs, webhooks)
make manifests

# Generate DeepCopy methods
make generate
```

## Architecture

### Main Controller Flow (main.go:156-201)

The controller initializes an OCM `AddonManager` and registers three addon agents:
- `policyframework.GetAndAddAgent`
- `configpolicy.GetAndAddAgent`
- `standalonetemplating.GetAndAddAgent`

Each agent is wrapped in a `PolicyAgentAddon` that intercepts the `Manifests()` method to check for the `policy-addon-pause=true` annotation, which short-circuits automatic addon updates.

### Addon Structure

Each addon in `pkg/addon/{addonname}/` follows a consistent pattern:

1. **agent_addon.go** - Main addon implementation with:
   - `getValues()` - Builds Helm values from cluster state, addon annotations, and defaults
   - `mandateValues()` - Sets required values that can't be overridden (e.g., replica count for old K8s)
   - `GetAgentAddon()` - Creates the addon using `addonfactory.NewAgentAddonFactory()`
   - `GetAndAddAgent()` - Wrapper to add the agent to the manager

2. **manifests/** - Contains:
   - `hubpermissions/` - RBAC for the addon to access hub resources
   - `managedclusterchart/` - Helm chart deployed to managed clusters

### Key Addon Configuration

Addons support configuration via:
- **Annotations on ManagedClusterAddOn**: `log-level`, `policy-evaluation-concurrency`, `client-qps`, `client-burst`, `prometheus-metrics-enabled`, etc.
- **ManagedCluster annotations**: `addon.open-cluster-management.io/on-multicluster-hub`
- **Helm values override**: `addon.open-cluster-management.io/values` annotation
- **AddonDeploymentConfig**: For node placement, resource requirements, custom variables

### Common Package (pkg/addon/common.go)

Shared utilities:
- `NewRegistrationOption()` - Creates addon registration with CSR configuration and RBAC
- `GetClusterVendor()` - Determines if cluster is OpenShift (from labels/claims)
- `IsOldKubernetes()` - Checks if cluster runs K8s < 1.14 (affects leader election)
- `GetLogLevel()` - Validates and parses log level annotations
- `PolicyAgentAddon` wrapper - Implements pause annotation check

### Hosted Mode

The controller supports hosted mode where addon components run on the hub cluster but manage a different cluster. This is detected via the `addon.open-cluster-management.io/hosting-cluster-name` annotation. When in hosted mode:
- The addon's `InstallNamespace` is used directly
- The hosting cluster's vendor/distribution is checked separately from the managed cluster

## Test Structure

E2E tests use Ginkgo and are in `test/e2e/`:
- `case1_framework_deployment_test.go` - Tests policy framework addon deployment
- `case2_config_deployment_test.go` - Tests config policy controller deployment
- `case3_standalonetemplating_test.go` - Tests standalone hub templating addon

Tests use a dynamic Kubernetes client to interact with the hub and managed clusters. The test suite sets up multiple managed clusters and validates addon deployments, ManifestWorks, and deployed resources.

## Important Notes

- The addon-controller automatically reverts changes to ManifestWorks to match Helm chart renders. Use the `policy-addon-pause=true` annotation on the ManagedClusterAddOn to persist ManifestWork changes during testing.
- Environment variables `CONFIG_POLICY_CONTROLLER_IMAGE` and `GOVERNANCE_POLICY_FRAMEWORK_ADDON_IMAGE` control which addon images are deployed.
- Before addons can be deployed, the work-agent must be started (use `make wait-for-work-agent`).
- The controller requires OCM registration-operator to be installed first.
- RBAC changes require running `make manifests` to regenerate manifests and `make kind-regenerate-controller` to apply.
