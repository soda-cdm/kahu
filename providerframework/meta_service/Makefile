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
META_SERVICE_PROTO := proto/metaservice.proto
META_SERVICE_GO := metaservice.pb.go
META_SERVICE_GO_TMP := proto/$(META_SERVICE_GO)
META_SERVICE_INTERFACE := $(shell pwd)/lib/go/$(META_SERVICE_GO)

# This recipe generates the go language bindings to a temp area.
$(META_SERVICE_INTERFACE): HERE := $(shell pwd)
$(META_SERVICE_INTERFACE): GO_OUT := plugins=grpc
$(META_SERVICE_INTERFACE): GO_OUT := $(GO_OUT):"$(HERE)"
$(META_SERVICE_INTERFACE): $(META_SERVICE_PROTO)
	($(PROTOC) --go_out=$(GO_OUT) --plugin=protoc-gen-go=$(PROTOC_GEN_GO) $(META_SERVICE_PROTO))

$(META_SERVICE_GO): $(META_SERVICE_INTERFACE)

build: $(META_SERVICE_GO)

clean:
	rm $(META_SERVICE_INTERFACE)

.PHONY: clean
