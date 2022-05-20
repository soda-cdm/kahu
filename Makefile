all: build

########################################################################
##                             GOLANG                                 ##
########################################################################

# If GOPATH isn't defined then set its default location.
ifeq (,$(strip $(GOPATH)))
GOPATH := $(HOME)/go
else
# If GOPATH is already set then update GOPATH to be its own
# first element.
GOPATH := $(word 1,$(subst :, ,$(GOPATH)))
endif
export GOPATH


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
$(PROTO_DIR):
	mkdir -p "$(PROTO_BIN_DIR)"

PROTOC := $(PROTO_BIN_DIR)/protoc
PROTOC_ZIP := protoc-$(PROTOC_VER)-$(PROTOC_OS)-$(PROTOC_ARCH).zip
PROTOC_URL := https://github.com/google/protobuf/releases/download/v$(PROTOC_VER)/$(PROTOC_ZIP)
PROTOC_TMP_BIN := $(PROTO_BIN_DIR)/protoc

$(PROTOC): $(PROTO_DIR)
	-curl -L $(PROTOC_URL) -o "$(PROTO_DIR)/$(PROTOC_ZIP)" && \
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
$(PROTOC_GEN_GO): PROTOBUF_VERSION := v1.3.2
$(PROTOC_GEN_GO): $(PROTO_DIR)
	mkdir -p $(dir $(GOPATH)/src/$(PROTOBUF_PKG))
	test -d $(GOPATH)/src/$(PROTOBUF_PKG)/.git || git clone https://$(PROTOBUF_PKG) $(GOPATH)/src/$(PROTOBUF_PKG)
	(cd $(GOPATH)/src/$(PROTOBUF_PKG) && \
		(test "$$(git describe --tags | head -1)" = "$(PROTOBUF_VERSION)" || \
			(git fetch && git checkout tags/$(PROTOBUF_VERSION))))
	(cd $(GOPATH)/src/$(PROTOBUF_PKG) && go get -v -d $$(go list -f '{{ .ImportPath }}' ./...)) && \
    	go mod tidy && \
	go build -o "$@" $(PROTOC_GEN_GO_PKG)

build_meta_service_grpc: $(PROTOC) $(PROTOC_GEN_GO)
	$(MAKE) -C provider/meta_service metaservice.pb.go PROTOC=$(PROTOC) PROTOC_GEN_GO=$(PROTOC_GEN_GO)

build_grpc_interface: build_meta_service_grpc
	go mod download


build: build_grpc_interface
