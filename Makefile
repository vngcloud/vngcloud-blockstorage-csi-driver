# golang-client Makefile
# Follows the interface defined in the Golang CTI proposed
# in https://review.openstack.org/410355

#REPO_VERSION?=$(shell git describe --tags)

GIT_HOST = github.com/vngcloud

CONTAINER_ENGINE ?= docker

PWD := $(shell pwd)
BASE_DIR := $(shell basename $(PWD))
# Keep an existing GOPATH, make a private one if it is undefined
GOPATH_DEFAULT := $(PWD)/.go
export GOPATH ?= $(GOPATH_DEFAULT)
GOBIN_DEFAULT := $(GOPATH)/bin
export GOBIN ?= $(GOBIN_DEFAULT)
export GO111MODULE := on
TESTARGS_DEFAULT := "-v"
export TESTARGS ?= $(TESTARGS_DEFAULT)
PKG := $(shell awk '/^module/ { print $$2 }' go.mod)
DEST := $(GOPATH)/src/$(GIT_HOST)/$(BASE_DIR)
SOURCES := Makefile go.mod go.sum $(shell find $(DEST) -name '*.go' 2>/dev/null)
HAS_GOX := $(shell command -v gox;)
GOX_PARALLEL ?= 3

TARGETS		?= linux/amd64 linux/386 linux/arm linux/arm64 linux/ppc64le linux/s390x
DIST_DIRS	= find * -type d -exec

TEMP_DIR	:=$(shell mktemp -d)
TAR_FILE	?= rootfs.tar

GOOS		?= $(shell go env GOOS)
GOPROXY		?= $(shell go env GOPROXY)
#VERSION     ?= $(shell git describe --tags --match='v*')
VERSION     ?= "v0.0.0"
GOARCH		:=
GOFLAGS		:=
TAGS		:=
LDFLAGS		:= "-w -s -X 'k8s.io/component-base/version.gitVersion=$(VERSION)' -X 'github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/driver.driverVersion=$(VERSION)'"
GOX_LDFLAGS	:= $(shell echo "$(LDFLAGS) -extldflags \"-static\"")
REGISTRY	?= vcr.vngcloud.vn/60108-cuongdm3
IMAGE_OS	?= linux
IMAGE_NAMES	?= vngcloud-blockstorage-csi-driver
ARCH		?= amd64
ARCHS		?= amd64 arm arm64 ppc64le s390x
BUILD_CMDS	?= vngcloud-blockstorage-csi-driver
# CTI targets

$(GOBIN):
	echo "create gobin"
	mkdir -p $(GOBIN)

work: $(GOBIN)

build-all-archs:
	@for arch in $(ARCHS); do $(MAKE) ARCH=$${arch} build ; done

build: $(BUILD_CMDS)

$(BUILD_CMDS): $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) GOPROXY=${GOPROXY} go build \
		-trimpath \
		-ldflags $(LDFLAGS) \
		-o $@ \
		cmd/$@/main.go

test: unit functional

check: work
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.56.0 run ./...

unit: work
	go test -tags=unit $(shell go list ./... | sed -e '/sanity/ { N; d; }' | sed -e '/tests/ {N; d;}') $(TESTARGS)

functional:
	@echo "$@ not yet implemented"

# kept for compatibility reasons.
fmt: check
lint: check
vet: check

cover: work
	go test -tags=unit $(shell go list ./...) -cover

docs:
	@echo "$@ not yet implemented"

godoc:
	@echo "$@ not yet implemented"

releasenotes:
	@echo "Reno not yet implemented for this repo"

translation:
	@echo "$@ not yet implemented"

# Do the work here

# Set up the development environment
env:
	@echo "PWD: $(PWD)"
	@echo "BASE_DIR: $(BASE_DIR)"
	@echo "GOPATH: $(GOPATH)"
	@echo "GOROOT: $(GOROOT)"
	@echo "DEST: $(DEST)"
	@echo "PKG: $(PKG)"
	go version
	go env

# Get our dev/test dependencies in place
bootstrap:
	tools/test-setup.sh

.bindep:
	virtualenv .bindep
	.bindep/bin/pip install -i https://pypi.python.org/simple bindep

bindep: .bindep
	@.bindep/bin/bindep -b -f bindep.txt || true

install-distro-packages:
	tools/install-distro-packages.sh

clean:
	rm -rf _dist .bindep
	@echo "clean builds binary"
	@for binary in $(BUILD_CMDS); do rm -rf $${binary}*; done

realclean: clean
	rm -rf vendor
	if [ "$(GOPATH)" = "$(GOPATH_DEFAULT)" ]; then \
		rm -rf $(GOPATH); \
	fi

shell:
	$(SHELL) -i

# Build a single image for the local default platform and push to the local
# container engine
build-local-image-%:
	$(CONTAINER_ENGINE) build \
		--build-arg VERSION=$(VERSION) \
		--tag $(REGISTRY)/$*:$(VERSION) \
		.

push-image-%:
	$(CONTAINER_ENGINE) image push $(REGISTRY)/$*:$(VERSION)

# Build all images locally
build-local-images: $(addprefix build-local-image-,$(IMAGE_NAMES))

push-local-images: $(addprefix push-image-,$(IMAGE_NAMES))

# Build a single image for all architectures in ARCHS and push it to REGISTRY
push-multiarch-image-%:
	$(CONTAINER_ENGINE) buildx build --output type=registry \
		--build-arg VERSION=$(VERSION) \
		--tag $(REGISTRY)/$*:$(VERSION) \
		--platform $(shell echo $(addprefix linux/,$(ARCHS)) | sed 's/ /,/g') \
		--target $* \
		.

# Push all multiarch images
push-multiarch-images: $(addprefix push-multiarch-image-,$(IMAGE_NAMES))

version:
	@echo ${VERSION}

.PHONY: build-cross
build-cross: work
ifndef HAS_GOX
	echo "installing gox"
	go install github.com/mitchellh/gox
endif
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(GOX_LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/openstack-cloud-controller-manager/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(GOX_LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/cinder-csi-plugin/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(GOX_LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/k8s-keystone-auth/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(GOX_LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/client-keystone-auth/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(GOX_LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/octavia-ingress-controller/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(GOX_LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/manila-csi-plugin/
	CGO_ENABLED=0 gox -parallel=$(GOX_PARALLEL) -output="_dist/{{.OS}}-{{.Arch}}/{{.Dir}}" -osarch='$(TARGETS)' $(GOFLAGS) $(if $(TAGS),-tags '$(TAGS)',) -ldflags '$(GOX_LDFLAGS)' $(GIT_HOST)/$(BASE_DIR)/cmd/magnum-auto-healer/

.PHONY: dist
dist: build-cross
	( \
		cd _dist && \
		$(DIST_DIRS) cp ../LICENSE {} \; && \
		$(DIST_DIRS) cp ../README.md {} \; && \
		$(DIST_DIRS) tar -zcf cloud-provider-openstack-$(VERSION)-{}.tar.gz {} \; && \
		$(DIST_DIRS) zip -r cloud-provider-openstack-$(VERSION)-{}.zip {} \; \
	)

.PHONY: bindep build clean cover work docs fmt functional lint realclean \
	relnotes test translation version build-cross dist codeclimate