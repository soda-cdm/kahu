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
PROVIDER_SERVICE_PROTO := proto/providerservice.proto
PROVIDER_SERVICE_GO := providerservice.pb.go
PROVIDER_SERVICE_INTERFACE := $(shell pwd)/lib/go/$(PROVIDER_SERVICE_GO)
GOPATH :=$(shell pwd)/../_output/go

# This recipe generates the go language bindings to a temp area.
$(PROVIDER_SERVICE_INTERFACE): HERE := $(shell pwd)
$(PROVIDER_SERVICE_INTERFACE): GO_OUT := plugins=grpc
$(PROVIDER_SERVICE_INTERFACE): GO_OUT := $(GO_OUT),Mgoogle/protobuf/descriptor.proto=github.com/golang/protobuf/protoc-gen-go/descriptor
$(PROVIDER_SERVICE_INTERFACE): GO_OUT := $(GO_OUT),Mk8s.io/api/core/v1/generated.proto=k8s.io/api/core/v1
$(PROVIDER_SERVICE_INTERFACE): GO_OUT := $(GO_OUT),Mk8s.io/apimachinery/pkg/api/resource/generated.proto=k8s.io/apimachinery/pkg/api/resource
$(PROVIDER_SERVICE_INTERFACE): GO_OUT := $(GO_OUT),Mk8s.io/apimachinery/pkg/runtime/generated.proto=k8s.io/apimachinery/pkg/runtime
$(PROVIDER_SERVICE_INTERFACE): GO_OUT := $(GO_OUT),Mk8s.io/apimachinery/pkg/runtime/schema/generated.proto=k8s.io/apimachinery/pkg/runtime/schema
$(PROVIDER_SERVICE_INTERFACE): GO_OUT := $(GO_OUT),Mk8s.io/apimachinery/pkg/apis/meta/v1/generated.proto=k8s.io/apimachinery/pkg/apis/meta/v1
$(PROVIDER_SERVICE_INTERFACE): GO_OUT := $(GO_OUT),Mk8s.io/apimachinery/pkg/util/intstr/generated.proto=k8s.io/apimachinery/pkg/util/intstr
$(PROVIDER_SERVICE_INTERFACE): GO_OUT := $(GO_OUT):"$(HERE)"
$(PROVIDER_SERVICE_INTERFACE): INCLUDE := -I$(GOPATH)/src:$(shell pwd)
$(PROVIDER_SERVICE_INTERFACE): $(PROVIDER_SERVICE_PROTO)
	($(PROTOC) $(INCLUDE) --go_out=$(GO_OUT) --plugin=protoc-gen-go=$(PROTOC_GEN_GO) $(PROVIDER_SERVICE_PROTO))

$(PROVIDER_SERVICE_GO):  $(PROVIDER_SERVICE_INTERFACE)

build: $(PROVIDER_SERVICE_GO)

clean:
	rm $(PROVIDER_SERVICE_INTERFACE)

.PHONY: clean

