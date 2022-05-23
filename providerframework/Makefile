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
PROVIDER_PROTO := proto/providerservice.proto
PROVIDER_GO := providerservice.pb.go
PROVIDER_GO_TMP := proto/$(PROVIDER_GO)
PROVIDER_INTERFACE := $(shell pwd)/lib/go/$(PROVIDER_GO)

# This recipe generates the go language bindings to a temp area.
$(PROVIDER_INTERFACE): HERE := $(shell pwd)
$(PROVIDER_INTERFACE): GO_OUT := plugins=grpc
$(PROVIDER_INTERFACE): GO_OUT := $(GO_OUT):"$(HERE)"
$(PROVIDER_INTERFACE): $(PROVIDER_PROTO)
	($(PROTOC) --go_out=$(GO_OUT) --plugin=protoc-gen-go=$(PROTOC_GEN_GO) $(PROVIDER_PROTO))

$(PROVIDER_GO): $(PROVIDER_INTERFACE)

build: $(PROVIDER_GO)

clean:
	rm $(PROVIDER_INTERFACE)

.PHONY: clean
