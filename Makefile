TARGETS := $(shell ls scripts)
DAPPER_IMAGE ?= pasturestack-host-provisioner-dapper:ubuntu26
DAPPER_SOURCE ?= /go/src/github.com/PastureStack/host-provisioner
DOCKER_BUILD_NETWORK ?= host
UBUNTU_MIRROR ?= http://archive.ubuntu.com/ubuntu

.dapper-image: Dockerfile.dapper
	docker build \
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
