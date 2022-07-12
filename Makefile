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
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test `go list ./... | grep -v test/e2e` -coverprofile coverage.out

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
HUB_KUBECONFIG ?= $(PWD)/$(KIND_NAME).kubeconfig-internal
MANAGED_CLUSTER_NAME ?= cluster1
ifneq ($(KIND_VERSION), latest)
	KIND_ARGS = --image kindest/node:$(KIND_VERSION)
else
	KIND_ARGS =
endif

.PHONY: kind-create-cluster
kind-create-cluster: $(KIND_KUBECONFIG) ## Create a kind cluster.

$(KIND_KUBECONFIG):
	@echo "creating cluster"
	kind create cluster --name $(KIND_NAME) $(KIND_ARGS)
	kind get kubeconfig --name $(KIND_NAME) > $(KIND_KUBECONFIG)

$(HUB_KUBECONFIG):
	@echo "fetching internal kubeconfig"
	kind get kubeconfig --name $(KIND_NAME) --internal > $(HUB_KUBECONFIG)

.PHONY: kind-delete-cluster
kind-delete-cluster: ## Delete a kind cluster.
	kind delete cluster --name $(KIND_NAME) || true
	rm $(KIND_KUBECONFIG) || true
	rm $(HUB_KUBECONFIG) || true

REGISTRATION_OPERATOR = $(PWD)/.go/registration-operator
$(REGISTRATION_OPERATOR):
	@mkdir -p .go
	git clone --depth 1 https://github.com/open-cluster-management-io/registration-operator.git .go/registration-operator

.PHONY: kind-deploy-registration-operator-hub
kind-deploy-registration-operator-hub: $(REGISTRATION_OPERATOR) $(KIND_KUBECONFIG) $(HUB_KUBECONFIG) ## Deploy the ocm registration operator to the kind cluster.
	cd $(REGISTRATION_OPERATOR) && KUBECONFIG=$(KIND_KUBECONFIG) make deploy-hub
	@printf "\n*** Pausing and waiting to let everything deploy ***\n\n"
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBEWAIT) -r deploy/cluster-manager -n open-cluster-management -c condition=Available -m 90
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBEWAIT) -r deploy/cluster-manager-placement-controller -n open-cluster-management-hub -c condition=Available -m 90

.PHONY: kind-deploy-registration-operator-managed
kind-deploy-registration-operator-managed: $(REGISTRATION_OPERATOR) $(KIND_KUBECONFIG) ## Deploy the ocm registration operator to the kind cluster.
	cd $(REGISTRATION_OPERATOR) && KUBECONFIG=$(KIND_KUBECONFIG) MANAGED_CLUSTER_NAME=$(MANAGED_CLUSTER_NAME) HUB_KUBECONFIG=$(HUB_KUBECONFIG) make deploy-spoke

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
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl get ns $(CONTROLLER_NAMESPACE); if [ $$? -ne 0 ] ; then kubectl create ns $(CONTROLLER_NAMESPACE); fi 
	go run ./main.go controller --kubeconfig=$(KIND_KUBECONFIG) --namespace $(CONTROLLER_NAMESPACE)

.PHONY: kind-load-image
kind-load-image: build-images $(KIND_KUBECONFIG) ## Build and load the docker image into kind.
	kind load docker-image $(IMAGE_NAME_AND_VERSION) --name $(KIND_NAME)

.PHONY: kind-regenerate-controller
kind-regenerate-controller: manifests generate kustomize $(KIND_KUBECONFIG) ## Refresh (or initially deploy) the policy-addon-controller.
	cp config/default/kustomization.yaml config/default/kustomization.yaml.tmp
	cd config/default && $(KUSTOMIZE) edit set image policy-addon-image=$(IMAGE_NAME_AND_VERSION)
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUSTOMIZE) build config/default | kubectl apply -f -
	mv config/default/kustomization.yaml.tmp config/default/kustomization.yaml
	KUBECONFIG=$(KIND_KUBECONFIG) kubectl delete -n $(CONTROLLER_NAMESPACE) pods -l=app=governance-policy-addon-controller

DEPLOYMENT_TARGETS := kind-deploy-registration-operator-hub kind-deploy-registration-operator-managed \
											kind-approve-cluster kind-load-image kind-regenerate-controller
.PHONY: kind-deploy-controller
kind-deploy-controller: $(DEPLOYMENT_TARGETS) ## Deploy the policy-addon-controller to the kind cluster.

GINKGO = $(LOCAL_BIN)/ginkgo
.PHONY: e2e-dependencies
e2e-dependencies: ## Download ginkgo locally if necessary.
	$(call go-get-tool,github.com/onsi/ginkgo/v2/ginkgo@$(shell awk '/github.com\/onsi\/ginkgo\/v2/ {print $$2}' go.mod))

.PHONY: e2e-test
e2e-test: e2e-dependencies
	$(GINKGO) -v --fail-fast --slow-spec-threshold=10s test/e2e

.PHONY: e2e-debug
e2e-debug: ## Collect debug logs from deployed clusters.
	@echo "##### Gathering information from $(KIND_NAME) #####"
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl get managedclusteraddons --all-namespaces
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n $(CONTROLLER_NAMESPACE) get deployments
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n $(CONTROLLER_NAMESPACE) get pods
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n open-cluster-management-agent-addon get deployments
	-KUBECONFIG=$(KIND_KUBECONFIG) kubectl -n open-cluster-management-agent-addon get pods
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
