PWD := $(shell pwd)
LOCAL_BIN ?= $(PWD)/bin
export PATH := $(LOCAL_BIN):$(PATH)
GOARCH = $(shell go env GOARCH)
GOOS = $(shell go env GOOS)
TESTARGS_DEFAULT := -v
TESTARGS ?= $(TESTARGS_DEFAULT)
CONTROLLER_NAME = $(shell cat COMPONENT_NAME 2> /dev/null)
CONTROLLER_NAMESPACE ?= open-cluster-management
# Handle KinD configuration
KIND_NAME ?= policy-addon-ctrl1
CLUSTER_NAME ?= cluster1
KIND_KUBECONFIG ?= $(PWD)/kubeconfig_$(CLUSTER_NAME)_e2e
KIND_KUBECONFIG_INTERNAL ?= $(PWD)/kubeconfig_$(CLUSTER_NAME)_e2e-internal
KIND_KUBECONFIG_SA ?= $(PWD)/kubeconfig_$(CLUSTER_NAME)
HUB_CLUSTER_NAME ?= cluster1
HUB_KUBECONFIG ?= $(PWD)/kubeconfig_$(HUB_CLUSTER_NAME)_e2e
HUB_KUBECONFIG_INTERNAL ?= $(HUB_KUBECONFIG)-internal

# Image URL to use all building/pushing image targets;
# Use your own docker registry and image name for dev/test by overridding the IMG and REGISTRY environment variable.
IMG ?= $(CONTROLLER_NAME)
REGISTRY ?= quay.io/open-cluster-management
TAG ?= latest
VERSION ?= $(shell cat COMPONENT_VERSION 2> /dev/null)
IMAGE_NAME_AND_VERSION ?= $(REGISTRY)/$(IMG)

# Fix sed issues on mac by using GSED
SED="sed"
ifeq ($(GOOS), darwin)
  SED="gsed"
endif

include build/common/Makefile.common.mk

# Strip v prefix from common Makefile version for OCM repo
KUSTOMIZE_VERSION_CLEAN := $(KUSTOMIZE_VERSION:v%=%)

############################################################
# Lint
############################################################

.PHONY: lint
lint:

.PHONY: fmt
fmt:

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

############################################################
# test section
############################################################

.PHONY: test
test:
	go test $(TESTARGS) `go list ./... | grep -v test/e2e`

.PHONY: test-coverage
test-coverage: TESTARGS = -json -cover -covermode=atomic -coverprofile=coverage_unit.out
test-coverage: test

.PHONY: gosec-scan
gosec-scan:

############################################################
# build section
############################################################

.PHONY: build
build: ## Build manager binary.
	CGO_ENABLED=1 go build -o build/_output/bin/$(IMG) main.go

############################################################
# images section
############################################################

.PHONY: build-images
build-images: generate fmt vet
	@docker build -t ${IMAGE_NAME_AND_VERSION} -f build/Dockerfile .
	@docker tag ${IMAGE_NAME_AND_VERSION} $(REGISTRY)/$(IMG):$(TAG)

############################################################
# clean section
############################################################

.PHONY: clean
clean: kind-bootstrap-delete-clusters ## Clean up generated files.
	-rm bin/*
	-rm build/_output/bin/*
	-rm coverage*.out
	-rm kubeconfig_*
	-rm -r vendor/
	-rm -rf .go/*

############################################################
# Generate manifests
############################################################

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=governance-policy-addon-controller crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

############################################################
# e2e test section
############################################################

KUBEWAIT ?= $(PWD)/build/common/scripts/kubewait.sh
ignore-not-found ?= false

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

.PHONY: kind-deploy-addons
kind-deploy-addons: ## Apply ManagedClusterAddon manifests to hub to deploy governance addons to a managed cluster.
	@echo "Creating ManagedClusterAddon for managed cluster $(CLUSTER_NAME)"
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl apply -f test/resources/config_policy_addon_cr.yaml -n $(CLUSTER_NAME)
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl apply -f test/resources/framework_addon_cr.yaml -n $(CLUSTER_NAME)

.PHONY: kind-create-cluster
kind-create-cluster: $(KIND_KUBECONFIG) ## Create a kind cluster.

$(KIND_KUBECONFIG):
	@echo "creating cluster"
	kind create cluster --name $(KIND_NAME) $(KIND_ARGS)
	kind get kubeconfig --name $(KIND_NAME) > $(KIND_KUBECONFIG)
	kind get kubeconfig --name $(KIND_NAME) --internal > $(KIND_KUBECONFIG_INTERNAL)
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl apply -f \
		https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.64.1/example/prometheus-operator-crd-full/monitoring.coreos.com_servicemonitors.yaml

.PHONY: kind-delete-cluster
kind-delete-cluster: ## Delete a kind cluster.
	-kind delete cluster --name $(KIND_NAME)
	-rm $(KIND_KUBECONFIG_SA)
	-rm $(KIND_KUBECONFIG)
	-rm $(KIND_KUBECONFIG_INTERNAL)

OCM_REPO = $(PWD)/.go/ocm
$(OCM_REPO):
	@mkdir -p .go
	git clone --depth 1 https://github.com/open-cluster-management-io/ocm.git .go/ocm

.PHONY: kind-deploy-registration-operator-hub
kind-deploy-registration-operator-hub: $(OCM_REPO) $(KIND_KUBECONFIG) ## Deploy the ocm registration operator to the kind cluster.
	cd $(OCM_REPO) && KUBECONFIG=$(KIND_KUBECONFIG) KUSTOMIZE_VERSION=$(KUSTOMIZE_VERSION_CLEAN) make deploy-hub
	@printf "\n*** Pausing and waiting to let everything deploy ***\n\n"
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBEWAIT) -r deploy/cluster-manager -n open-cluster-management -c condition=Available -m 90
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBEWAIT) -r deploy/cluster-manager-placement-controller -n open-cluster-management-hub -c condition=Available -m 90
	@echo installing Policy CRD on hub
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl apply -f \
		https://raw.githubusercontent.com/open-cluster-management-io/governance-policy-propagator/main/deploy/crds/policy.open-cluster-management.io_policies.yaml

.PHONY: kind-deploy-registration-operator-managed
kind-deploy-registration-operator-managed: $(OCM_REPO) $(KIND_KUBECONFIG) ## Deploy the ocm registration operator to the kind cluster.
	cd $(OCM_REPO) && \
	KUBECONFIG=$(KIND_KUBECONFIG) MANAGED_CLUSTER_NAME=$(CLUSTER_NAME) HUB_KUBECONFIG=$(HUB_KUBECONFIG_INTERNAL) \
	KUSTOMIZE_VERSION=$(KUSTOMIZE_VERSION_CLEAN) make deploy-spoke-operator
	cd $(OCM_REPO) && \
	KUBECONFIG=$(KIND_KUBECONFIG) MANAGED_CLUSTER_NAME=$(CLUSTER_NAME) HUB_KUBECONFIG=$(HUB_KUBECONFIG_INTERNAL) \
	KUSTOMIZE_VERSION=$(KUSTOMIZE_VERSION_CLEAN) make apply-spoke-cr

.PHONY: kind-deploy-registration-operator-managed-hosted
kind-deploy-registration-operator-managed-hosted: $(OCM_REPO) $(KIND_KUBECONFIG) ## Deploy the ocm registration operator to the kind cluster in hosted mode.
	cd $(OCM_REPO) && \
	KUBECONFIG=$(HUB_KUBECONFIG) MANAGED_CLUSTER_NAME=$(CLUSTER_NAME) HUB_KUBECONFIG=$(HUB_KUBECONFIG_INTERNAL) \
	HOSTED_CLUSTER_MANAGER_NAME=$(HUB_CLUSTER_NAME) EXTERNAL_MANAGED_KUBECONFIG=$(KIND_KUBECONFIG_INTERNAL) \
	KUSTOMIZE_VERSION=$(KUSTOMIZE_VERSION_CLEAN) make deploy-spoke-hosted

.PHONY: kind-approve-cluster
kind-approve-cluster: $(KIND_KUBECONFIG) ## Approve managed cluster in the kind cluster.
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBEWAIT) -r "csr -l open-cluster-management.io/cluster-name=$(CLUSTER_NAME)" -m 120
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl certificate approve "$$(KUBECONFIG=$(KIND_KUBECONFIG) kubectl get csr -l open-cluster-management.io/cluster-name=$(CLUSTER_NAME) -o name)"
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBEWAIT) -r managedcluster/$(CLUSTER_NAME) -n open-cluster-management -m 60
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl patch managedcluster $(CLUSTER_NAME) -p='{"spec":{"hubAcceptsClient":true}}' --type=merge

.PHONY: wait-for-work-agent
wait-for-work-agent: $(KIND_KUBECONFIG) ## Wait for the klusterlet work agent to start.
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBEWAIT) -r "pod -l=app=klusterlet-agent" -n open-cluster-management-agent -c condition=Ready -m 360

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
	$(KUSTOMIZE) build config/default | $(SED) -E "s/(value\: .+)(:latest)$$/\1:$(TAG)/g" | KUBECONFIG=$(KIND_KUBECONFIG) kubectl apply -f -
	mv config/default/kustomization.yaml.tmp config/default/kustomization.yaml
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl delete -n $(CONTROLLER_NAMESPACE) pods -l=app=governance-policy-addon-controller

OCM_PREP_TARGETS := kind-deploy-registration-operator-hub kind-deploy-registration-operator-managed kind-approve-cluster
.PHONY: kind-prep-ocm
kind-prep-ocm: $(OCM_PREP_TARGETS) ## Install OCM registration pieces and connect the clusters

.PHONY: kind-deploy-controller
kind-deploy-controller: kind-prep-ocm kind-load-image kind-regenerate-controller ## Deploy the policy-addon-controller to the kind cluster.

.PHONY: e2e-test
e2e-test: e2e-dependencies ## Run E2E tests.
	$(GINKGO) -v --label-filter="!hosted-mode" --fail-fast $(E2E_TEST_ARGS) test/e2e

.PHONY: e2e-test-hosted-mode
e2e-test-hosted-mode: e2e-dependencies
	$(GINKGO) -v --label-filter="hosted-mode" --fail-fast test/e2e

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
	  GOVERNANCE_POLICY_FRAMEWORK_ADDON_IMAGE="$(REGISTRY)/governance-policy-framework-addon:$(TAG)" \
	  ./build/_output/bin/$(IMG)-instrumented -test.v -test.run="^TestRunMain$$" -test.coverprofile=$(COVERAGE_E2E_OUT) \
	  --kubeconfig="$(KIND_KUBECONFIG_SA)" $(LOG_REDIRECT) &

.PHONY: e2e-stop-instrumented
e2e-stop-instrumented:
	ps -ef | grep '$(IMG)' | grep -v grep | awk '{print $$2}' | xargs kill -s SIGUSR1
	sleep 5 # wait for tests to gracefully shut down
	-ps -ef | grep '$(IMG)' | grep -v grep | awk '{print $$2}' | xargs kill

.PHONY: e2e-debug
e2e-debug: ## Collect debug logs from deployed clusters.
	##### Gathering information from $(KIND_NAME) #####
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl get managedclusters
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl get managedclusteraddons --all-namespaces
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n $(CONTROLLER_NAMESPACE) get all
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n open-cluster-management-agent get all
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n open-cluster-management-agent-addon get all
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl get manifestwork --all-namespaces -o yaml
	
	## Local controller log:
	-cat build/_output/controller.log
	## Container logs in namespace $(CONTROLLER_NAMESPACE):
	-@for POD in $(shell KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n $(CONTROLLER_NAMESPACE) get pods -o name); do \
		for CONTAINER in $$(KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n $(CONTROLLER_NAMESPACE) get $${POD} -o jsonpath={.spec.containers[*].name}); do \
			echo "## Logs for pod $${POD} from container $${CONTAINER} in namespace $(CONTROLLER_NAMESPACE)"; \
			KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n $(CONTROLLER_NAMESPACE) logs $${POD}; \
		done; \
	done
	## Container logs in namespace open-cluster-management-agent-addon:
	-@for POD in $(shell KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n open-cluster-management-agent-addon get pods -o name); do \
		for CONTAINER in $$(KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n open-cluster-management-agent-addon get $${POD} -o jsonpath={.spec.containers[*].name}); do \
			echo "## Logs for pod $${POD} from container $${CONTAINER} in namespace open-cluster-management-agent-addon"; \
			KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n open-cluster-management-agent-addon logs $${POD}; \
		done; \
	done

.PHONY: install-resources
install-resources: kustomize
	@echo creating namespace
	-kubectl create ns $(CONTROLLER_NAMESPACE)
	@echo deploying roles and service account
	kustomize build config/rbac | $(SED) "s/namespace: system/namespace: open-cluster-management/g" | kubectl -n $(CONTROLLER_NAMESPACE) apply -o yaml -f -
	@echo deploying InternalHubComponent CRD
	kubectl apply -f https://raw.githubusercontent.com/stolostron/multiclusterhub-operator/refs/heads/main/config/crd/bases/operator.open-cluster-management.io_internalhubcomponents.yaml

.PHONY: kind-ensure-sa
kind-ensure-sa: export KUBECONFIG=$(KIND_KUBECONFIG_SA)

.PHONY: kind-controller-kubeconfig
kind-controller-kubeconfig: export CLUSTER_NAME=$(HUB_CLUSTER_NAME)

############################################################
# test coverage
############################################################

COVERAGE_FILE = coverage.out
.PHONY: coverage-merge
coverage-merge: coverage-dependencies ## Merge coverage reports.
	@echo Merging the coverage reports into $(COVERAGE_FILE)
	$(GOCOVMERGE) $(PWD)/coverage_* > $(COVERAGE_FILE)

.PHONY: coverage-verify
coverage-verify: ## Verify coverage percentage meets coverage thresholds.
	./build/common/scripts/coverage_calc.sh
