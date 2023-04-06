PWD := $(shell pwd)
LOCAL_BIN ?= $(PWD)/bin

# Image URL to use all building/pushing image targets;
# Use your own docker registry and image name for dev/test by overridding the IMG and REGISTRY environment variable.
IMG ?= $(shell cat COMPONENT_NAME 2> /dev/null)
REGISTRY ?= quay.io/open-cluster-management
TAG ?= latest
VERSION ?= $(shell cat COMPONENT_VERSION 2> /dev/null)
IMAGE_NAME_AND_VERSION ?= $(REGISTRY)/$(IMG)

GOARCH = $(shell go env GOARCH)
GOOS = $(shell go env GOOS)

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.23

# Keep an existing GOPATH, make a private one if it is undefined
GOPATH_DEFAULT := $(PWD)/.go
export GOPATH ?= $(GOPATH_DEFAULT)
GOBIN_DEFAULT := $(GOPATH)/bin
export GOBIN ?= $(GOBIN_DEFAULT)
export PATH=$(LOCAL_BIN):$(GOBIN):$(shell echo $$PATH)

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# go-get-tool will 'go install' any package $1 and install it to LOCAL_BIN.
define go-get-tool
@set -e ;\
echo "Checking installation of $(1)" ;\
GOBIN=$(LOCAL_BIN) go install $(1)
endef

.PHONY: all
all: build

include build/common/Makefile.common.mk

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: clean
clean: ## Clean up generated files.
	-rm bin/*
	-rm build/_output/bin/*
	-rm coverage*.out
	-rm *.kubeconfig
	-rm *.kubeconfig-internal
	-rm -r vendor/

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test $(TESTARGS) `go list ./... | grep -v test/e2e`

.PHONY: test-coverage
test-coverage: TESTARGS = -json -cover -covermode=atomic -coverprofile=coverage_unit.out
test-coverage: test

GOSEC = $(LOCAL_BIN)/gosec
GOSEC_VERSION = 2.9.6

.PHONY: gosec
gosec:
	$(call go-get-tool,github.com/securego/gosec/v2/cmd/gosec@v2.9.6)

.PHONY: gosec-scan
gosec-scan: gosec ## Run a gosec scan against the code.
	$(GOSEC) -fmt sonarqube -out gosec.json -no-fail -exclude-dir=.go ./...

##@ Build

.PHONY: build
build: ## Build manager binary.
	@build/common/scripts/gobuild.sh build/_output/bin/$(IMG) main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

.PHONY: build-images
build-images: generate fmt vet
	@docker build -t ${IMAGE_NAME_AND_VERSION} -f build/Dockerfile .
	@docker tag ${IMAGE_NAME_AND_VERSION} $(REGISTRY)/$(IMG):$(TAG)

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMAGE_NAME_AND_VERSION}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

CONTROLLER_GEN = $(LOCAL_BIN)/controller-gen
.PHONY: controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0)

KUSTOMIZE = $(LOCAL_BIN)/kustomize
.PHONY: kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,sigs.k8s.io/kustomize/kustomize/v4@v4.5.4)

ENVTEST = $(LOCAL_BIN)/setup-envtest
.PHONY: envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-get-tool,sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

KUBEWAIT ?= $(PWD)/build/common/scripts/kubewait.sh

##@ Kind

KIND_NAME ?= policy-addon-ctrl1
KIND_KUBECONFIG ?= $(PWD)/$(KIND_NAME).kubeconfig
KIND_KUBECONFIG_INTERNAL ?= $(PWD)/$(KIND_NAME).kubeconfig-internal
HUB_KUBECONFIG ?= $(PWD)/policy-addon-ctrl1.kubeconfig
HUB_KUBECONFIG_INTERNAL ?= $(PWD)/policy-addon-ctrl1.kubeconfig-internal
HUB_CLUSTER_NAME ?= policy-addon-ctrl1
MANAGED_CLUSTER_NAME ?= cluster1
KIND_VERSION ?= latest
KUSTOMIZE_VERSION ?= v5.0.0
# Set the Kind version tag
ifdef KIND_VERSION
  ifeq ($(KIND_VERSION), minimum)
    KIND_ARGS = --image kindest/node:v1.19.16
  else ifneq ($(KIND_VERSION), latest)
    KIND_ARGS = --image kindest/node:$(KIND_VERSION)
  endif
endif

.PHONY: kind-bootstrap-cluster
kind-bootstrap-cluster: ## Bootstrap Kind clusters and load clusters with locally built images.
	RUN_MODE="create" ./build/manage-clusters.sh

.PHONY: kind-bootstrap-cluster-dev
kind-bootstrap-cluster-dev: ## Bootstrap Kind clusters without loading images so code can be run locally.
	RUN_MODE="create-dev" ./build/manage-clusters.sh

.PHONY: kind-bootstrap-delete-clusters
kind-bootstrap-delete-clusters: ## Delete clusters created from a bootstrap target.
	RUN_MODE="delete" ./build/manage-clusters.sh

.PHONY: kind-bootstrap-deploy-addons
kind-bootstrap-deploy-addons: ## Deploy addons to bootstrap clusters.
	RUN_MODE="deploy-addons" ./build/manage-clusters.sh

.PHONY: kind-deploy-addons-hub
kind-deploy-addons-hub: kind-deploy-addons-managed ## Apply ManagedClusterAddon manifests to hub to deploy governance addons to a managed hub cluster.
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl annotate ManagedClusterAddon governance-policy-framework addon.open-cluster-management.io/on-multicluster-hub='true' -n $(MANAGED_CLUSTER_NAME)

.PHONY: kind-deploy-addons-managed
kind-deploy-addons-managed: ## Apply ManagedClusterAddon manifests to hub to deploy governance addons to a managed cluster.
	@echo "Creating ManagedClusterAddon for managed cluster $(MANAGED_CLUSTER_NAME)"
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl apply -f test/resources/config_policy_addon_cr.yaml -n $(MANAGED_CLUSTER_NAME)
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl apply -f test/resources/framework_addon_cr.yaml -n $(MANAGED_CLUSTER_NAME)

.PHONY: kind-create-cluster
kind-create-cluster: $(KIND_KUBECONFIG) ## Create a kind cluster.

$(KIND_KUBECONFIG):
	@echo "creating cluster"
	kind create cluster --name $(KIND_NAME) $(KIND_ARGS)
	kind get kubeconfig --name $(KIND_NAME) > $(KIND_KUBECONFIG)
	kind get kubeconfig --name $(KIND_NAME) --internal > $(KIND_KUBECONFIG_INTERNAL)
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl apply -f https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.57.0/example/prometheus-operator-crd-full/monitoring.coreos.com_servicemonitors.yaml

.PHONY: kind-delete-cluster
kind-delete-cluster: ## Delete a kind cluster.
	-kind delete cluster --name $(KIND_NAME)
	-rm $(KIND_KUBECONFIG)
	-rm $(KIND_KUBECONFIG_INTERNAL)

REGISTRATION_OPERATOR = $(PWD)/.go/registration-operator
$(REGISTRATION_OPERATOR):
	@mkdir -p .go
	git clone --depth 1 https://github.com/open-cluster-management-io/registration-operator.git .go/registration-operator

.PHONY: kind-deploy-registration-operator-hub
kind-deploy-registration-operator-hub: $(REGISTRATION_OPERATOR) $(KIND_KUBECONFIG) ## Deploy the ocm registration operator to the kind cluster.
	cd $(REGISTRATION_OPERATOR) && KUBECONFIG=$(KIND_KUBECONFIG) KUSTOMIZE_VERSION=$(KUSTOMIZE_VERSION) make deploy-hub
	@printf "\n*** Pausing and waiting to let everything deploy ***\n\n"
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBEWAIT) -r deploy/cluster-manager -n open-cluster-management -c condition=Available -m 90
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBEWAIT) -r deploy/cluster-manager-placement-controller -n open-cluster-management-hub -c condition=Available -m 90
	@echo installing Policy CRD on hub
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl apply -f https://raw.githubusercontent.com/open-cluster-management-io/governance-policy-propagator/main/deploy/crds/policy.open-cluster-management.io_policies.yaml

.PHONY: kind-deploy-registration-operator-managed
kind-deploy-registration-operator-managed: $(REGISTRATION_OPERATOR) $(KIND_KUBECONFIG) ## Deploy the ocm registration operator to the kind cluster.
	cd $(REGISTRATION_OPERATOR) && KUBECONFIG=$(KIND_KUBECONFIG) MANAGED_CLUSTER_NAME=$(MANAGED_CLUSTER_NAME) HUB_KUBECONFIG=$(HUB_KUBECONFIG_INTERNAL) KUSTOMIZE_VERSION=$(KUSTOMIZE_VERSION) make deploy-spoke

.PHONY: kind-deploy-registration-operator-managed-hosted
kind-deploy-registration-operator-managed-hosted: $(REGISTRATION_OPERATOR) $(KIND_KUBECONFIG) ## Deploy the ocm registration operator to the kind cluster in hosted mode.
	cd $(REGISTRATION_OPERATOR) && KUBECONFIG=$(HUB_KUBECONFIG) MANAGED_CLUSTER_NAME=$(MANAGED_CLUSTER_NAME) HUB_KUBECONFIG=$(HUB_KUBECONFIG_INTERNAL) HOSTED_CLUSTER_MANAGER_NAME=$(HUB_CLUSTER_NAME) EXTERNAL_MANAGED_KUBECONFIG=$(KIND_KUBECONFIG_INTERNAL) KUSTOMIZE_VERSION=$(KUSTOMIZE_VERSION) make deploy-spoke-hosted

.PHONY: kind-approve-cluster
kind-approve-cluster: $(KIND_KUBECONFIG) ## Approve managed cluster in the kind cluster.
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBEWAIT) -r "csr -l open-cluster-management.io/cluster-name=$(MANAGED_CLUSTER_NAME)" -m 120
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl certificate approve "$$(KUBECONFIG=$(KIND_KUBECONFIG) kubectl get csr -l open-cluster-management.io/cluster-name=$(MANAGED_CLUSTER_NAME) -o name)"
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBEWAIT) -r managedcluster/$(MANAGED_CLUSTER_NAME) -n open-cluster-management -m 60
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl patch managedcluster $(MANAGED_CLUSTER_NAME) -p='{"spec":{"hubAcceptsClient":true}}' --type=merge

.PHONY: wait-for-work-agent
wait-for-work-agent: $(KIND_KUBECONFIG) ## Wait for the klusterlet work agent to start.
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBEWAIT) -r "pod -l=app=klusterlet-manifestwork-agent" -n open-cluster-management-agent -c condition=Ready -m 360

CONTROLLER_NAMESPACE ?= governance-policy-addon-controller-system

.PHONY: kind-run-local
kind-run-local: manifests generate fmt vet $(KIND_KUBECONFIG) ## Run the policy-addon-controller locally against the kind cluster.
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl create ns $(CONTROLLER_NAMESPACE)
	go run ./main.go controller --kubeconfig=$(KIND_KUBECONFIG) --namespace $(CONTROLLER_NAMESPACE)

.PHONY: kind-load-image
kind-load-image: build-images $(KIND_KUBECONFIG) ## Build and load the docker image into kind.
	kind load docker-image $(IMAGE_NAME_AND_VERSION) --name $(KIND_NAME)

.PHONY: kind-regenerate-controller
kind-regenerate-controller: manifests generate kustomize $(KIND_KUBECONFIG) ## Refresh (or initially deploy) the policy-addon-controller.
	cp config/default/kustomization.yaml config/default/kustomization.yaml.tmp
	cd config/default && $(KUSTOMIZE) edit set image policy-addon-image=$(IMAGE_NAME_AND_VERSION)
	$(KUSTOMIZE) build config/default | sed -E "s/(value\: .+)(:latest)$$/\1:$(TAG)/g" | KUBECONFIG=$(KIND_KUBECONFIG) kubectl apply -f -
	mv config/default/kustomization.yaml.tmp config/default/kustomization.yaml
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl delete -n $(CONTROLLER_NAMESPACE) pods -l=app=governance-policy-addon-controller

OCM_PREP_TARGETS := kind-deploy-registration-operator-hub kind-deploy-registration-operator-managed kind-approve-cluster
.PHONY: kind-prep-ocm
kind-prep-ocm: $(OCM_PREP_TARGETS) ## Install OCM registration pieces and connect the clusters

.PHONY: kind-deploy-controller
kind-deploy-controller: kind-prep-ocm kind-load-image kind-regenerate-controller ## Deploy the policy-addon-controller to the kind cluster.

GINKGO = $(LOCAL_BIN)/ginkgo
.PHONY: e2e-dependencies
e2e-dependencies: ## Download ginkgo locally if necessary.
	$(call go-get-tool,github.com/onsi/ginkgo/v2/ginkgo@$(shell awk '/github.com\/onsi\/ginkgo\/v2/ {print $$2}' go.mod))

.PHONY: e2e-test
e2e-test: e2e-dependencies ## Run E2E tests.
	$(GINKGO) -v --label-filter="!hosted-mode" --fail-fast --slow-spec-threshold=10s $(E2E_TEST_ARGS) test/e2e

.PHONY: e2e-test-hosted-mode
e2e-test-hosted-mode: e2e-dependencies
	$(GINKGO) -v --label-filter="hosted-mode" --fail-fast --slow-spec-threshold=10s test/e2e

.PHONY: e2e-test-coverage
e2e-test-coverage: E2E_TEST_ARGS = --json-report=report_e2e.json --output-dir=.
e2e-test-coverage: e2e-run-instrumented e2e-test e2e-stop-instrumented ## Run E2E tests using instrumented controller.

.PHONY: e2e-test-coverage-foreground
e2e-test-coverage-foreground: LOG_REDIRECT =
e2e-test-coverage-foreground: e2e-test-coverage ## Run E2E tests using instrumented controller with logging to stdin.

.PHONY: e2e-build-instrumented
e2e-build-instrumented:
	go test -covermode=atomic -coverpkg=$(shell cat go.mod | head -1 | cut -d ' ' -f 2)/... -c -tags e2e ./ -o build/_output/bin/$(IMG)-instrumented

COVERAGE_E2E_OUT ?= coverage_e2e.out
.PHONY: e2e-run-instrumented
LOG_REDIRECT ?= &>build/_output/controller.log
e2e-run-instrumented: e2e-build-instrumented
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl create ns $(CONTROLLER_NAMESPACE)
	CONFIG_POLICY_CONTROLLER_IMAGE="$(REGISTRY)/config-policy-controller:$(TAG)" \
	  KUBE_RBAC_PROXY_IMAGE="registry.redhat.io/openshift4/ose-kube-rbac-proxy:v4.10" \
	  GOVERNANCE_POLICY_FRAMEWORK_ADDON_IMAGE="$(REGISTRY)/governance-policy-framework-addon:$(TAG)" \
	  ./build/_output/bin/$(IMG)-instrumented -test.v -test.run="^TestRunMain$$" -test.coverprofile=$(COVERAGE_E2E_OUT) \
	  --kubeconfig="$(KIND_KUBECONFIG)" $(LOG_REDIRECT) &

.PHONY: e2e-stop-instrumented
e2e-stop-instrumented:
	ps -ef | grep '$(IMG)' | grep -v grep | awk '{print $$2}' | xargs kill -s SIGUSR1
	sleep 5 # wait for tests to gracefully shut down
	-ps -ef | grep '$(IMG)' | grep -v grep | awk '{print $$2}' | xargs kill

.PHONY: e2e-debug
e2e-debug: ## Collect debug logs from deployed clusters.
	@echo "##### Gathering information from $(KIND_NAME) #####"
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl get managedclusteraddons --all-namespaces
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n $(CONTROLLER_NAMESPACE) get deployments
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n $(CONTROLLER_NAMESPACE) get pods
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n open-cluster-management-agent-addon get deployments
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n open-cluster-management-agent-addon get pods
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl get manifestwork --all-namespaces -o yaml
	
	@echo "* Local controller log:"
	-cat build/_output/controller.log
	@echo "* Container logs in namespace $(CONTROLLER_NAMESPACE):"
	-@for POD in $(shell KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n $(CONTROLLER_NAMESPACE) get pods -o name); do \
		for CONTAINER in $$(KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n $(CONTROLLER_NAMESPACE) get $${POD} -o jsonpath={.spec.containers[*].name}); do \
			echo "* Logs for pod $${POD} from container $${CONTAINER} in namespace $(CONTROLLER_NAMESPACE)"; \
			KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n $(CONTROLLER_NAMESPACE) logs $${POD}; \
		done; \
	done
	@echo "* Container logs in namespace open-cluster-management-agent-addon:"
	-@for POD in $(shell KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n open-cluster-management-agent-addon get pods -o name); do \
		for CONTAINER in $$(KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n open-cluster-management-agent-addon get $${POD} -o jsonpath={.spec.containers[*].name}); do \
			echo "* Logs for pod $${POD} from container $${CONTAINER} in namespace open-cluster-management-agent-addon"; \
			KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n open-cluster-management-agent-addon logs $${POD}; \
		done; \
	done

.PHONY: fmt-dependencies
fmt-dependencies:
	$(call go-get-tool,github.com/daixiang0/gci@v0.2.9)
	$(call go-get-tool,mvdan.cc/gofumpt@v0.2.0)

# All available format: format-go format-protos format-python
# Default value will run all formats, override these make target with your requirements:
#    eg: fmt: format-go format-protos
.PHONY: fmt
fmt: fmt-dependencies
	find . -not \( -path "./.go" -prune -or -path "./vendor" -prune \) -name "*.go" | xargs gofmt -s -w
	find . -not \( -path "./.go" -prune -or -path "./vendor" -prune \) -name "*.go" | xargs gci -w -local "$(shell cat go.mod | head -1 | cut -d " " -f 2)"
	find . -not \( -path "./.go" -prune -or -path "./vendor" -prune \) -name "*.go" | xargs gofumpt -l -w

##@ Quality Control
lint-dependencies:
	$(call go-get-tool,github.com/golangci/golangci-lint/cmd/golangci-lint@v1.46.2)

# All available linters: lint-dockerfiles lint-scripts lint-yaml lint-copyright-banner lint-go lint-python lint-helm lint-markdown lint-sass lint-typescript lint-protos
# Default value will run all linters, override these make target with your requirements:
#    eg: lint: lint-go lint-yaml
.PHONY: lint
lint: lint-dependencies lint-all ## Run linting against the code.

##@ Test Coverage
GOCOVMERGE = $(LOCAL_BIN)/gocovmerge
.PHONY: coverage-dependencies
coverage-dependencies:
	$(call go-get-tool,github.com/wadey/gocovmerge@v0.0.0-20160331181800-b5bfa59ec0ad)

COVERAGE_FILE = coverage.out
.PHONY: coverage-merge
coverage-merge: coverage-dependencies ## Merge coverage reports.
	@echo Merging the coverage reports into $(COVERAGE_FILE)
	$(GOCOVMERGE) $(PWD)/coverage_* > $(COVERAGE_FILE)

.PHONY: coverage-verify
coverage-verify: ## Verify coverage percentage meets coverage thresholds.
	./build/common/scripts/coverage_calc.sh
