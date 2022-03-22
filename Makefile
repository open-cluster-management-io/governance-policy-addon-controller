
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))

# Image URL to use all building/pushing image targets;
# Use your own docker registry and image name for dev/test by overridding the IMG and REGISTRY environment variable.
IMG ?= $(shell cat COMPONENT_NAME 2> /dev/null)
REGISTRY ?= quay.io/stolostron
TAG ?= latest
VERSION ?= $(shell cat COMPONENT_VERSION 2> /dev/null)
IMAGE_NAME_AND_VERSION ?= $(REGISTRY)/$(IMG):$(VERSION)

GOARCH = $(shell go env GOARCH)
GOOS = $(shell go env GOOS)

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.23

PWD := $(shell pwd)
BASE_DIR := $(shell basename $(PWD))
export PATH=$(PWD)/bin:$(shell echo $$PATH)
# Keep an existing GOPATH, make a private one if it is undefined
GOPATH_DEFAULT := $(PWD)/.go
export GOPATH ?= $(GOPATH_DEFAULT)
GOBIN_DEFAULT := $(GOPATH)/bin
export GOBIN ?= $(GOBIN_DEFAULT)

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

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

GOSEC = $(PWD)/bin/gosec
GOSEC_VERSION = 2.9.6

$(GOSEC):
	mkdir -p $(PWD)/bin
	curl -L https://github.com/securego/gosec/releases/download/v$(GOSEC_VERSION)/gosec_$(GOSEC_VERSION)_$(GOOS)_$(GOARCH).tar.gz | tar -xz -C $(PWD)/bin gosec

.PHONY: gosec-scan
gosec-scan: $(GOSEC) ## Run a gosec scan against the code.
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

CONTROLLER_GEN = $(PWD)/bin/controller-gen
.PHONY: controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0)

KUSTOMIZE = $(PWD)/bin/kustomize
.PHONY: kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

ENVTEST = $(PWD)/bin/setup-envtest
.PHONY: envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-get-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

# go-get-tool will 'go get' any package $2 and install it to $1.
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

KUBEWAIT ?= $(PWD)/build/common/scripts/kubewait.sh

##@ Kind

KIND_NAME ?= policy-addon-ctrl1
KIND_KUBECONFIG ?= $(PWD)/$(KIND_NAME).kubeconfig
HUB_KUBECONFIG ?= $(PWD)/$(KIND_NAME).kubeconfig-internal
MANAGED_CLUSTER_NAME ?= cluster1

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
	KUBECONFIG=$(KIND_KUBECONFIG) $(KUBEWAIT) -r "csr -l open-cluster-management.io/cluster-name=$(MANAGED_CLUSTER_NAME)" -m 60
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

.PHONY: regenerate-controller
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

GINKGO = $(PWD)/bin/ginkgo
.PHONY: e2e-dependencies
e2e-dependencies: ## Download ginkgo locally if necessary.
	$(call go-get-tool,$(GINKGO),github.com/onsi/ginkgo/v2/ginkgo@v2.1.3)

.PHONY: e2e-test
e2e-test: e2e-dependencies
	$(GINKGO) -v --fail-fast --slow-spec-threshold=10s test/e2e

.PHONY: fmt-dependencies
fmt-dependencies:
	$(call go-get-tool,$(PWD)/bin/gci,github.com/daixiang0/gci@v0.2.9)
	$(call go-get-tool,$(PWD)/bin/gofumpt,mvdan.cc/gofumpt@v0.2.0)

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
	$(call go-get-tool,$(PWD)/bin/golangci-lint,github.com/golangci/golangci-lint/cmd/golangci-lint@v1.41.1)

# All available linters: lint-dockerfiles lint-scripts lint-yaml lint-copyright-banner lint-go lint-python lint-helm lint-markdown lint-sass lint-typescript lint-protos
# Default value will run all linters, override these make target with your requirements:
#    eg: lint: lint-go lint-yaml
.PHONY: lint
lint: lint-dependencies lint-all ## Run linting against the code.
