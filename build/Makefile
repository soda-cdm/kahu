# Copyright 2022 The SODA Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#      http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# It's necessary to link sh -> bash.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# ignore make's built-in rules.
MAKEFLAGS += --no-builtin-rules
.SUFFIXES:

# Constants used throughout.
.EXPORT_ALL_VARIABLES:
OUT_DIR ?= _output
BIN_DIR := $(OUT_DIR)/bin
PRJ_SRC_PATH := github.com/soda-cdm/kahu
GENERATED_FILE_EXT := pb.go
BASE_IMAGE_REGISTRY=docker.io
VERSION ?= v0.1.0
PLATFORM ?= X86
ARCH ?= amd64
OS ?= linux

ifeq (${PLATFORM}, ARM)
	ARCH = arm64
endif

platform := ${OS}/${ARCH}

# This controls the verbosity of the build.  Higher numbers mean more output.
KAHU_VERBOSE ?= 1
CODE_GENERATOR_VERSION="v0.22.2"

# set git sha and tree state
GIT_SHA = $(shell git rev-parse HEAD)
ifneq ($(shell git status --porcelain 2> /dev/null),)
	GIT_TREE_STATE ?= dirty
else
	GIT_TREE_STATE ?= clean
endif

define ALL_HELP_INFO
# Build code.
#
# Args:
#   WHAT: Directory names to build. The build will produce executable
#			files under $(OUT_DIR)/bin. If not specified, "everything" will be built.
#   GOFLAGS: Extra flags to pass to 'go' when building.
#   GOLDFLAGS: Extra linking flags passed to 'go' when building.
#   GOGCFLAGS: Additional go compile flags passed to 'go' when building.
#
# Example:
#   make
#   make all
#   make all WHAT=cmd/controllers GOFLAGS=-v
#   make all GOLDFLAGS=""
#     Note: Specify GOLDFLAGS as an empty string for building binaries, which allows
#           you to use code debugging tools like delve.
endef
.PHONY: all
ifeq ($(PRINT_HELP),y)
all:
	@echo "$$ALL_HELP_INFO"
else
all: verify
	@build/build.sh $(WHAT)
endif

define CLEAN_HELP_INFO
# Remove all build artifacts.
#
# Example:
#   make clean
#
endef
.PHONY: clean
ifeq ($(PRINT_HELP),y)
clean:
	@echo "$$CLEAN_HELP_INFO"
else
clean:
	@build/make-clean.sh && \
$(MAKE) -f Makefile.generate_proto clean
endif

define RELEASE_IMAGES_HELP_INFO
# Build release images
#
# Example:
#   make release-images
endef
.PHONY: release-images
ifeq ($(PRINT_HELP),y)
release-images:
	@echo "$$RELEASE_IMAGES_HELP_INFO"
else
release-images: all
	@build/release-images.sh
endif

define GENERATED_FILES_HELP_INFO
# Produce auto-generated files needed for the build.
#
# Example:
#   make generated_files
endef
.PHONY: generated_files
ifeq ($(PRINT_HELP),y)
generated_files:
	@echo "$$GENERATED_FILES_HELP_INFO"
else
generated_files: verify
	$(MAKE) -f Makefile.generate_proto $@
endif

define VERIFY_HELP_INFO
# Runs all the presubmission verifications.
#
# Example:
#   make verify
endef
.PHONY: verify
ifeq ($(PRINT_HELP),y)
verify:
	@echo "$$VERIFY_HELP_INFO"
else
verify:
	@build/verify.sh
endif

.PHONY: verify-fmt
verify-fmt:
	@hack/go-format.sh verify

.PHONY: update-fmt
update-fmt:
	@hack/go-format.sh

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
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd:generateEmbeddedObjectMeta=true webhook paths="./apis/..." output:crd:artifacts:config=config/crd/v1beta1/bases

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/${OUT_DIR}/bin go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

