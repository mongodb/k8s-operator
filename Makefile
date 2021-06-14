SHELL := /bin/bash

# Image URL to use all building/pushing image targets
REPO_URL := $(shell jq -r .repo_url < ~/.community-operator-dev/config.json)
OPERATOR_IMAGE := $(shell jq -r .operator_image < ~/.community-operator-dev/config.json)
NAMESPACE := $(shell jq -r .namespace < ~/.community-operator-dev/config.json)
IMG := $(REPO_URL)/$(OPERATOR_IMAGE)
DOCKERFILE ?= operator
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=true,crdVersions=v1beta1"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: manager

# Run unit tests
TEST ?= ./pkg/... ./api/... ./cmd/... ./controllers/... ./test/e2e/util/mongotester/...
ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test: generate fmt vet manifests
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.7.0/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); go test $(TEST) -coverprofile cover.out

# Build manager binary
manager: generate fmt vet
	go build -o bin/manager ./cmd/manager/main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: install
	$(KUSTOMIZE) build config/local_run | kubectl apply -n $(NAMESPACE) -f -
	go run ./cmd/manager/main.go

# Install CRDs into a cluster
install: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests kustomize
	cd config/manager && $(KUSTOMIZE) edit set image quay.io/mongodb/mongodb-kubernetes-operator=$(IMG):latest
	$(KUSTOMIZE) build config/default | kubectl apply -n $(NAMESPACE) -f -

# UnDeploy controller from the configured Kubernetes cluster in ~/.kube/config
undeploy:
	$(KUSTOMIZE) build config/default | kubectl delete -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) paths="./..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Run e2e tests locally using go build while also setting up a proxy in the shell to allow
# the test to run as if it were inside the cluster. This enables mongodb connectivity while running locally.
e2e-telepresence: install
	telepresence connect; \
	telepresence status; \
	eval $$(scripts/dev/get_e2e_env_vars.py $(cleanup)); \
    go test -v -timeout=30m -failfast ./test/e2e/$(test); \
	telepresence quit

# Run e2e test by deploying test image in kubernetes.
e2e-k8s: install e2e-image
	python scripts/dev/e2e.py --perform-cleanup --test $(test)

# Run e2e test locally.
# e.g. make e2e test=replica_set cleanup=true
e2e: install
	eval $$(scripts/dev/get_e2e_env_vars.py $(cleanup)); \
	go test -v -short -timeout=30m -failfast ./test/e2e/$(test)

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

# Build and push the operator image
operator-image:
	python pipeline.py --image-name operator-ubi

# Build and push e2e test image
e2e-image:
	python pipeline.py --image-name e2e

# Build and push agent image
agent-image:
	python pipeline.py --image-name agent-ubuntu

# Build and push readiness probe image
readiness-probe-image:
	python pipeline.py --image-name readiness-probe-init

# Build and push version upgrade post start hook image
version-upgrade-post-start-hook-image:
	python pipeline.py --image-name version-post-start-hook-init

# create all required images
all-images: operator-image e2e-image agent-image readiness-probe-image version-upgrade-post-start-hook-image


# Download controller-gen locally if necessary
CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen:
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1)

# Download kustomize locally if necessary
KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize:
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
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

install-prerequisites-macos:
	scripts/dev/install_prerequisites.sh
