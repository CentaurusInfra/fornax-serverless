# Image URL to use all building/pushing image targets
VERSION ?= v0.1.0

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
all: build test

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

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./pkg/apis/..." output:crd:artifacts:config=config/crd/bases

## Generate code for rest api resource model, containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
.PHONY: generate
generate: controller-gen openapi-gen client-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./pkg/apis/core/..."
	# $(OPENAPI_GEN) --go-header-file="hack/boilerplate.go.txt" --input-dirs="./pkg/apis/core/..." --output-package="centaurusinfra.io/fornax-serverless/pkg/apis/openapi"

 ## Generate client-go sdk containing clientset, lister, and informer method implementations.
GENERATE_GROUPS = $(shell pwd)/hack/generate-groups.sh
.PHONY: generate-client
generate-client-gen: client-gen
	$(GENERATE_GROUPS) "client, lister, informer"  centaurusinfra.io/fornax-serverless/pkg/client "centaurusinfra.io/fornax-serverless/pkg/apis" "core:v1" \
	--go-header-file hack/boilerplate.go.txt \

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: generate fmt vet envtest ## Run tests.
	go test ./... -coverprofile cover.out

##@ Build
LDFLAGS=-w -s
.PHONY: build
build: generate fmt vet ## Build binary.
	go build ./...
	go build -ldflags "$(LDFLAGS)" -o bin/integtestgrpcserver cmd/integtestgrpcserver/main.go
	go build -ldflags "$(LDFLAGS)" -o bin/fornaxcore cmd/fornaxcore/main.go
	go build -ldflags "$(LDFLAGS)" -o bin/nodeagent cmd/nodeagent/main.go
	go build -ldflags "$(LDFLAGS)" -o bin/simulatenode cmd/simulation/node/main.go
	go build -ldflags "$(LDFLAGS)" -o bin/fornaxtest cmd/fornaxtest/main.go

APISERVER-BOOT = $(shell pwd)/bin/apiserver-boot
.PHONY: debug-fornaxcore-local
debug-fornaxcore-local: build ## Download apiserver-boot cmd locally if necessary.
	# $(call go-get-tool,$(APISERVER-BOOT),sigs.k8s.io/apiserver-builder-alpha/cmd/apiserver-boot@v1.23.0)
	# $(APISERVER-BOOT) run local --run etcd,fornaxcore
	@ulimit -n 8192
	@nohup bin/fornaxcore --etcd-servers=http://127.0.0.1:2379 --secure-port=9443 --standalone-debug-mode --bind-address=127.0.0.1 --feature-gates=APIPriorityAndFairness=false > fornax-core.log 2

.PHONY: run
run: generate fmt vet ## Run from your host.

.PHONY: run-nodeagent-local
	@sudo ./bin/nodeagent --fornaxcore-ip localhost:18001 --disable-swap=false

APISERVER-BOOT = $(shell pwd)/bin/apiserver-boot
.PHONY: run-fornaxcore-local
run-fornaxcore-local: ## Download apiserver-boot cmd locally if necessary.
	 # export KUBERNETES_SERVICE_HOST=192.168.0.45
   # export KUBERNETES_SERVICE_PORT=9443
   # export KUBERNETES_MASTER=192.168.0.45
	 # @nohup bin/fornaxcore --etcd-servers=http://127.0.0.1:2379 --secure-port=9443 --kubeconfig ./kubeconfig.server --authentication-kubeconfig ./kubeconfig.server --requestheader-client-ca-file /var/lib/fornaxcore/certs/fornax-client.crt --tls-cert-file /var/lib/fornaxcore/certs/fornaxcore-server.crt --tls-private-key-file /var/lib/fornaxcore/certs/fornaxcore-server.key --client-ca-file /var/lib/fornaxcore/certs/fornaxcore.crt > fornax-core.log 2
	 @nohup bin/fornaxcore --etcd-servers=http://127.0.0.1:2379 --secure-port=9443 --standalone-debug-mode --bind-address=127.0.0.1 --feature-gates=APIPriorityAndFairness=false > fornax-core.log 2

.PHONY: docker-build
docker-build: test ## Build docker image
	@sudo docker build -f ./dockerimages/Dockerfile.echoserver -t centaurusinfra.io/fornax-serverless/echoserver:${VERSION} .
	@sudo docker build -f ./dockerimages/Dockerfile.sessionwrapper -t centaurusinfra.io/fornax-serverless/session-wrapper:${VERSION} .
	@sudo docker build -f ./dockerimages/nodejs-hw/Dockerfile.nodejs-hw -t centaurusinfra.io/fornax-serverless/nodejs-hw:${VERSION} .

.PHONY: docker-push
docker-push: ## Push docker image into docker hub registry
	@sudo docker push centaurusinfra.io/fornax-serverless/echoserver:${VERSION}
	@sudo docker push centaurusinfra.io/fornax-serverless/session-wrapper:${VERSION}
	@sudo docker push centaurusinfra.io/fornax-serverless/nodejs-hw:${VERSION}

## Push docker image into a local containerd for test
.PHONY: containerd-local-push
containerd-local-push: containerd-local-push-session-wrapper  containerd-local-push-echoserver containerd-local-push-nodejs-hw

.PHONY: containerd-local-push-session-wrapper
containerd-local-push-session-wrapper:
	@sudo docker image save -o /tmp/centaurusinfra.io.fornax-serverless.session-wrapper.img centaurusinfra.io/fornax-serverless/session-wrapper:${VERSION}
	@sudo crictl rmi centaurusinfra.io/fornax-serverless/session-wrapper:${VERSION}
	@sudo ctr -n=k8s.io image import /tmp/centaurusinfra.io.fornax-serverless.session-wrapper.img

.PHONY: containerd-local-push-echoserver
containerd-local-push-echoserver:
	@sudo docker image save -o /tmp/centaurusinfra.io.fornax-serverless.echoserver.img centaurusinfra.io/fornax-serverless/echoserver:${VERSION}
	@sudo crictl rmi centaurusinfra.io/fornax-serverless/echoserver:${VERSION}
	@sudo ctr -n=k8s.io image import /tmp/centaurusinfra.io.fornax-serverless.echoserver.img

.PHONY: containerd-local-push-nodejs-hw
containerd-local-push-nodejs-hw:
	@sudo docker image save -o /tmp/centaurusinfra.io.fornax-serverless.nodejs-hw.img centaurusinfra.io/fornax-serverless/nodejs-hw:${VERSION}
	@sudo crictl rmi centaurusinfra.io/fornax-serverless/nodejs-hw:${VERSION}
	@sudo ctr -n=k8s.io image import /tmp/centaurusinfra.io.fornax-serverless.nodejs-hw.img

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
	@rm -f bin/fornaxcore
	@rm -f bin/nodeagent
	@rm -f bin/simulatenode
	@rm -f bin/integtestgrpcserver
	@rm -f bin/fornaxtest

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: install-certs ## Install CRDs into the K8s cluster specified in ~/.kube/config.

.PHONY: install-certs
install-certs:
	@sudo mkdir -p /var/lib/fornaxcore/certs/
	@sudo openssl genrsa -out /tmp/ca.key 2048
	@sudo openssl req -x509 -new -nodes -key /tmp/ca.key -subj "/CN=192.168.0.45" -days 10000 -out /tmp/ca.crt
	@sudo openssl genrsa -out /tmp/server.key 2048
	@sudo openssl req -new -key /tmp/server.key -out /tmp/server.csr -config ./config/csr.conf
	@sudo openssl x509 -req -in /tmp/server.csr -CA /tmp/ca.crt -CAkey /tmp/ca.key \
     -CAcreateserial -out /tmp/server.crt -days 10000 \
     -extensions v3_ext -extfile ./config/csr.conf
	# @sudo openssl req  -noout -text -in /tmp/server.csr
	@sudo mv /tmp/ca.key /var/lib/fornaxcore/certs/fornaxcore.key
	@sudo mv /tmp/ca.crt /var/lib/fornaxcore/certs/fornaxcore.crt
	@sudo mv /tmp/server.key /var/lib/fornaxcore/certs/fornaxcore-server.key
	@sudo mv /tmp/server.crt /var/lib/fornaxcore/certs/fornaxcore-server.crt


.PHONY: uninstall
uninstall: ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.

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

# generate fornaxcore grpc code
PROTOC_GEN = $(shell pwd)/bin/protoc-gen-go
PROTOC_GEN_GRPC = $(shell pwd)/bin/protoc-gen-go-grpc
PROTOC = $(HOME)/.local/bin/protoc
.PHONY: protoc-gen
protoc-gen: ## Download protc-gen locally if necessary.
	$(call go-get-tool,$(PROTOC_GEN),google.golang.org/protobuf/cmd/protoc-gen-go@v1.28)
	$(call go-get-tool,$(PROTOC_GEN_GRPC),google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2)
	$(call get-protoc,$(PROTOC))
	go mod vendor
	$(PROTOC) -I=./ -I=./vendor \
		--go_out=../.. \
		--go-grpc_out=../../ \
		--go_opt=Mk8s.io/api/core/v1/generated.proto=k8s.io/api/core/v1 \
		--go_opt=Mk8s.io/apimachinery/pkg/api/resource/generated.proto=k8s.io/apimachinery/pkg/api/resource \
		--go_opt=Mk8s.io/apimachinery/pkg/apis/meta/v1/generated.proto=k8s.io/apimachinery/pkg/apis/meta/v1 \
		--go_opt=Mk8s.io/apimachinery/pkg/runtime/generated.proto=k8s.io/apimachinery/pkg/runtime \
		--go_opt=Mk8s.io/apimachinery/pkg/runtime/schema/generated.proto=k8s.io/apimachinery/pkg/runtime/schema \
		--go_opt=Mk8s.io/apimachinery/pkg/util/intstr/generated.proto=k8s.io/apimachinery/pkg/util/intstr \
		pkg/fornaxcore/grpc/fornaxcore.proto
	$(PROTOC) -I=./ -I=./vendor \
		--go_out=../.. \
		--go-grpc_out=../../ \
		pkg/nodeagent/sessionservice/grpc/session_service.proto

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
