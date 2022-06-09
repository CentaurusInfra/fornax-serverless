# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.23

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

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
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./pkg/..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen openapi-gen client-gen ## generate-client-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./pkg/..."
	# $(OPENAPI_GEN) --go-header-file="hack/boilerplate.go.txt" --input-dirs="./pkg/apis/core/..." --output-package="centaurusinfra.io/fornax-serverless/pkg/apis/openapi"

GENERATE_GROUPS = $(shell pwd)/hack/generate-groups.sh
.PHONY: generate-client-gen
generate-client-gen:  ## Generate code containing clientset, lister, and informer method implementations.
	$(GENERATE_GROUPS) "client, lister, informer"  centaurusinfra.io/fornax-serverless/pkg/client "centaurusinfra.io/fornax-serverless/pkg/apis" "core:v1" \
	--go-header-file hack/boilerplate.go.txt \

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./... -coverprofile cover.out

##@ Build

.PHONY: build
build: generate fmt vet ## Build binary.
	go build ./...
	go build -o bin/apiserver cmd/apiserver/main.go

.PHONY: run
run: manifests generate fmt vet ## Run from your host.

.PHONY: docker-build
docker-build: test ## Build docker image with the manager.
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}

.PHONY: check
check:
	@hostname
	@cat /etc/os-release
	@pwd
	@lscpu | grep CPU\(s\)
	@free -m
	make --version
	@echo "check is done"

.PHONY: clean
clean:
	@rm -rf config/
	@rm -f cover.out
	@rm -f bin/apiserver

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
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

APISERVER-BOOT = $(shell pwd)/bin/apiserver-boot
.PHONY: apiserver-boot
apiserver-local: ## Download apiserver-boot cmd locally if necessary.
	$(call go-get-tool,$(APISERVER-BOOT),sigs.k8s.io/apiserver-builder-alpha/cmd/apiserver-boot@v1.23.0)
	$(APISERVER-BOOT) run local --run etcd,apiserver

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
.PHONY: controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.9.0)

OPENAPI_GEN = $(shell pwd)/bin/openapi-gen
.PHONY: openapi-gen
openapi-gen: ## Download openapi-gen locally if necessary.
	$(call go-get-tool,$(OPENAPI_GEN),k8s.io/kube-openapi/cmd/openapi-gen@v0.0.0-20211115234752-e816edb12b65)

CLIENT_GEN = $(shell pwd)/bin/client-gen		## use it to generate clientset
LISTER_GEN = $(shell pwd)/bin/lister-gen		## use it to generate lister watch 
INFORMER_GEN = $(shell pwd)/bin/informer-gen    ## use it to generate informer info
.PHONY: client-gen
client-gen: ## Download client-gen, lister-gen and informer-gen locally if necessary.
	$(call go-get-tool,$(CLIENT_GEN),k8s.io/code-generator/cmd/client-gen@v0.23.1)
	$(call go-get-tool,$(LISTER_GEN),k8s.io/code-generator/cmd/lister-gen@v0.23.1)
	$(call go-get-tool,$(INFORMER_GEN),k8s.io/code-generator/cmd/informer-gen@v0.23.1)

PROTOC_GEN = $(shell pwd)/bin/protoc-gen-go
PROTOC_GEN_GRPC = $(shell pwd)/bin/protoc-gen-go-grpc
PROTOC = $(HOME)/.local/bin/protoc
.PHONY: protoc-gen
protoc-gen: ## Download protc-gen locally if necessary.
	$(call go-get-tool,$(PROTOC_GEN),google.golang.org/protobuf/cmd/protoc-gen-go@v1.28)
	$(call go-get-tool,$(PROTOC_GEN_GRPC),google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2)
	$(call get-protoc,$(PROTOC))
	$(PROTOC) -I=./ -I=./vendor \
		--go_out=../.. \
		--go-grpc_out=../../ \
		--go_opt=Mk8s.io/api/core/v1/generated.proto=k8s.io/api/core/v1 \
		--go_opt=Mk8s.io/apimachinery/pkg/api/resource/generated.proto=k8s.io/apimachinery/pkg/api/resource \
		--go_opt=Mk8s.io/apimachinery/pkg/apis/meta/v1/generated.proto=k8s.io/apimachinery/pkg/apis/meta/v1 \
		--go_opt=Mk8s.io/apimachinery/pkg/runtime/generated.proto=k8s.io/apimachinery/pkg/runtime \
		--go_opt=Mk8s.io/apimachinery/pkg/runtime/schema/generated.proto=k8s.io/apimachinery/pkg/runtime/schema \
		--go_opt=Mk8s.io/apimachinery/pkg/util/intstr/generated.proto=k8s.io/apimachinery/pkg/util/intstr \
		pkg/fornaxcore/fornaxcore.proto

KUSTOMIZE = $(shell pwd)/bin/kustomize
.PHONY: kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

ENVTEST = $(shell pwd)/bin/setup-envtest
.PHONY: envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-get-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

define get-protoc
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
PB_REL="https://github.com/protocolbuffers/protobuf/releases" ;\
curl -LO $$PB_REL/download/v3.12.1/protoc-3.12.1-linux-x86_64.zip ;\
echo "get protoc" ;\
unzip $$TMP_DIR/protoc-3.12.1-linux-x86_64.zip -d $$HOME/.local ;\
rm -rf $$TMP_DIR ;\
}
endef
