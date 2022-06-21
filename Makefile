# Copyright 2022 The SODA Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Constants used throughout.
.EXPORT_ALL_VARIABLES:
OUT_DIR ?= _output
BIN_DIR := $(OUT_DIR)/bin

CODE_GENERATOR_VERSION="v0.22.2"

########################################################################
##                             PROTOC                                 ##
########################################################################

# Only set PROTOC_VER if it has an empty value.
ifeq (,$(strip $(PROTOC_VER)))
PROTOC_VER := 3.9.1
endif

PROTOC_OS := $(shell uname -s)
ifeq (Darwin,$(PROTOC_OS))
PROTOC_OS := osx
endif

PROTOC_ARCH := $(shell uname -m)
ifeq (i386,$(PROTOC_ARCH))
PROTOC_ARCH := x86_32
endif

HERE := $(shell pwd)
PROTO_DIR := $(HERE)/.proto
PROTO_BIN_DIR := $(PROTO_DIR)/bin

PROTOC := $(PROTO_BIN_DIR)/protoc
PROTOC_ZIP := protoc-$(PROTOC_VER)-$(PROTOC_OS)-$(PROTOC_ARCH).zip
PROTOC_URL := https://github.com/google/protobuf/releases/download/v$(PROTOC_VER)/$(PROTOC_ZIP)
PROTOC_TMP_BIN := $(PROTO_BIN_DIR)/protoc

$(PROTOC):
	-mkdir -p "$(PROTO_BIN_DIR)" && \
      curl -L $(PROTOC_URL) -o "$(PROTO_DIR)/$(PROTOC_ZIP)" && \
	  unzip "$(PROTO_DIR)/$(PROTOC_ZIP)" -d "$(PROTO_DIR)" && \
	  chmod 0755 "$(PROTOC_TMP_BIN)" && \
	stat "$@" > /dev/null 2>&1

########################################################################
##                          PROTOC-GEN-GO                             ##
########################################################################

# This is the recipe for getting and installing the go plug-in
# for protoc
PROTOC_GEN_GO_PKG := github.com/golang/protobuf/protoc-gen-go
PROTOC_GEN_GO := $(PROTO_BIN_DIR)/protoc-gen-go
$(PROTOC_GEN_GO): PROTOBUF_PKG := $(dir $(PROTOC_GEN_GO_PKG))
$(PROTOC_GEN_GO): PROTOBUF_VERSION := v1.5.2
$(PROTOC_GEN_GO): $(PROTO_DIR)
	mkdir -p $(dir $(PROTO_DIR)/src/$(PROTOBUF_PKG))
	test -d $(PROTO_DIR)/src/$(PROTOBUF_PKG)/.git || git clone https://$(PROTOBUF_PKG) $(PROTO_DIR)/src/$(PROTOBUF_PKG)
	(cd $(PROTO_DIR)/src/$(PROTOBUF_PKG) && \
		(test "$$(git describe --tags | head -1)" = "$(PROTOBUF_VERSION)" || \
			(git fetch && git checkout tags/$(PROTOBUF_VERSION))))
	(cd $(PROTO_DIR)/src/$(PROTOBUF_PKG) && go get -v -d $$(go list -f '{{ .ImportPath }}' ./...)) && \
	go build -o "$@" $(PROTOC_GEN_GO_PKG)

build_meta_service_grpc: $(PROTOC) $(PROTOC_GEN_GO)
	$(MAKE) -C providerframework/meta_service metaservice.pb.go PROTOC=$(PROTOC) PROTOC_GEN_GO=$(PROTOC_GEN_GO)


grpc_interfaces: build_meta_service_grpc

build: grpc_interfaces

clean:
	rm -rf $(PROTO_DIR)

.PHONY: clean

# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.22

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

codegen:
	@echo "Generating CRD auto generated code"
	(GOFLAGS="" CODE_GENERATOR_VERSION=$(CODE_GENERATOR_VERSION) hack/update-codegen.sh)

CONTROLLER_GEN = $(shell pwd)/${OUT_DIR}/bin/controller-gen
.PHONY: controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0)

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./apis/..." output:crd:artifacts:config=config/crd/v1beta1/bases

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/${OUT_DIR}/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef
