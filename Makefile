TARGETS := $(shell ls scripts)
DAPPER_IMAGE ?= pasturestack-host-provisioner-dapper:ubuntu26
DAPPER_SOURCE ?= /go/src/github.com/PastureStack/host-provisioner
DOCKER_VERSION ?= 29.7.2
DOCKER_SHA256 ?= 803d433f226db4776e1768fd319fc6c6e4935a456acf84fcc0080818b854bc8f
DOCKER_BUILD_NETWORK ?= host
UBUNTU_MIRROR ?= http://archive.ubuntu.com/ubuntu

.dapper-image: Dockerfile.dapper
	docker build \
		--build-arg DOCKER_VERSION=$(DOCKER_VERSION) \
		--build-arg DOCKER_SHA256=$(DOCKER_SHA256) \
		--build-arg UBUNTU_MIRROR=$(UBUNTU_MIRROR) \
		--network $(DOCKER_BUILD_NETWORK) \
		-t $(DAPPER_IMAGE) \
		-f Dockerfile.dapper .

$(TARGETS): .dapper-image
	docker run --rm \
		-v $(CURDIR):$(DAPPER_SOURCE) \
		-e DAPPER_UID=$$(id -u) \
		-e DAPPER_GID=$$(id -g) \
		-e TAG \
		-e REPO \
		-e DOCKER_BUILD_NETWORK=$(DOCKER_BUILD_NETWORK) \
		-e UBUNTU_MIRROR=$(UBUNTU_MIRROR) \
		-e VERSION_OVERRIDE \
		-e SOURCE_DATE_EPOCH \
		$(DAPPER_IMAGE) $@

trash:
	@echo "Dependencies are vendored; no external dependency fetch is required."

trash-keep: trash

deps: trash

.DEFAULT_GOAL := ci

.PHONY: $(TARGETS) deps trash trash-keep
