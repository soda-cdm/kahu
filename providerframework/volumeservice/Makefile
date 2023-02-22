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

all: build

########################################################################
##                           BUILD GRPC INTERFACE                     ##
########################################################################
VOLUME_SERVICE_PROTO := proto/volumeservice.proto
VOLUME_SERVICE_GO := volumeservice.pb.go
VOLUME_SERVICE_GO_TMP := proto/$(VOLUME_SERVICE_GO)
VOLUME_SERVICE_INTERFACE := $(shell pwd)/lib/go/$(VOLUME_SERVICE_GO)
GOPATH :=$(shell pwd)/../..

# This recipe generates the go language bindings to a temp area.
$(VOLUME_SERVICE_INTERFACE): HERE := $(shell pwd)
$(VOLUME_SERVICE_INTERFACE): GO_OUT := plugins=grpc
$(VOLUME_SERVICE_INTERFACE): GO_OUT := $(GO_OUT),Mgoogle/protobuf/descriptor.proto=github.com/golang/protobuf/protoc-gen-go/descriptor
$(VOLUME_SERVICE_INTERFACE): GO_OUT := $(GO_OUT),Mk8s.io/api/core/v1/generated.proto=k8s.io/api/core/v1
$(VOLUME_SERVICE_INTERFACE): GO_OUT := $(GO_OUT),Mk8s.io/apimachinery/pkg/api/resource/generated.proto=k8s.io/apimachinery/pkg/api/resource
$(VOLUME_SERVICE_INTERFACE): GO_OUT := $(GO_OUT),Mk8s.io/apimachinery/pkg/runtime/generated.proto=k8s.io/apimachinery/pkg/runtime
$(VOLUME_SERVICE_INTERFACE): GO_OUT := $(GO_OUT),Mk8s.io/apimachinery/pkg/runtime/schema/generated.proto=k8s.io/apimachinery/pkg/runtime/schema
$(VOLUME_SERVICE_INTERFACE): GO_OUT := $(GO_OUT),Mk8s.io/apimachinery/pkg/apis/meta/v1/generated.proto=k8s.io/apimachinery/pkg/apis/meta/v1
$(VOLUME_SERVICE_INTERFACE): GO_OUT := $(GO_OUT),Mk8s.io/apimachinery/pkg/util/intstr/generated.proto=k8s.io/apimachinery/pkg/util/intstr
$(VOLUME_SERVICE_INTERFACE): GO_OUT := $(GO_OUT):"$(HERE)"
$(VOLUME_SERVICE_INTERFACE): INCLUDE := -I$(GOPATH)/vendor:$(shell pwd)
$(VOLUME_SERVICE_INTERFACE): $(VOLUME_SERVICE_PROTO)
	($(PROTOC) $(INCLUDE) --go_out=$(GO_OUT) --plugin=protoc-gen-go=$(PROTOC_GEN_GO) $(VOLUME_SERVICE_PROTO))

$(VOLUME_SERVICE_GO): $(VOLUME_SERVICE_INTERFACE)

build: $(VOLUME_SERVICE_GO)

clean:
	rm $(VOLUME_SERVICE_INTERFACE)

.PHONY: clean
