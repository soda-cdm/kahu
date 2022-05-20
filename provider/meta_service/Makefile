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
