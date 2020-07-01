# Tencent is pleased to support the open source community by making TKEStack
# available.
#
# Copyright (C) 2012-2019 Tencent. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may not use
# this file except in compliance with the License. You may obtain a copy of the
# License at
#
# https://opensource.org/licenses/Apache-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
# WARRANTIES OF ANY KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations under the License.

# ==============================================================================
# Makefile helper functions for docker image

DOCKER := DOCKER_CLI_EXPERIMENTAL=enabled docker
DOCKER_SUPPORTED_API_VERSION ?= 1.40
DOCKER_VERSION ?= 19.03

REGISTRY_PREFIX ?= tkestack

EXTRA_ARGS ?=
_DOCKER_BUILD_EXTRA_ARGS :=

ifdef HTTP_PROXY
_DOCKER_BUILD_EXTRA_ARGS += --build-arg http_proxy=${HTTP_PROXY}
endif
ifdef HTTPS_PROXY
_DOCKER_BUILD_EXTRA_ARGS += --build-arg https_proxy=${HTTPS_PROXY}
endif

ifneq ($(EXTRA_ARGS), )
_DOCKER_BUILD_EXTRA_ARGS += $(EXTRA_ARGS)
endif

.PHONY: image.verify
image.verify:
	$(eval API_VERSION := $(shell $(DOCKER) version | grep -E 'API version: {1,6}[0-9]' | head -n1 | awk '{print $$3} END { if (NR==0) print 0}' ))
	$(eval PASS := $(shell echo "$(API_VERSION) >= $(DOCKER_SUPPORTED_API_VERSION)" | bc))
	@if [ $(PASS) -ne 1 ]; then \
		$(DOCKER) -v ;\
		echo "Unsupported docker version. Docker API version should be greater than $(DOCKER_SUPPORTED_API_VERSION) (Or docker version: $(DOCKER_VERSION))"; \
		exit 1; \
	fi

.PHONY: image.buildx.verify
image.buildx.verify: image.verify
	$(eval PASS := $(shell $(DOCKER) buildx version > /dev/null && echo 1 || echo 0))
	@if [ $(PASS) -ne 1 ]; then \
		$(MAKE) image.buildx.install; \
	fi

.PHONY: image.buildx.install
image.buildx.install:
	@$(ROOT_DIR)/build/lib/install-buildx.sh

.PHONY: image.build
image.build: image.buildx.verify $(addprefix image.build., $(addprefix $(PLATFORM)., $(BINS)))

.PHONY: image.build.multiarch
image.build.multiarch: image.buildx.verify $(foreach p,$(PLATFORMS),$(addprefix image.build., $(addprefix $(p)., $(BINS))))

.PHONY: image.build.%
image.build.%:
	$(eval PLATFORM := $(word 1,$(subst ., ,$*)))
	$(eval IMAGE := $(word 2,$(subst ., ,$*)))
	$(eval OS := $(word 1,$(subst _, ,$(PLATFORM))))
	$(eval ARCH := $(word 2,$(subst _, ,$(PLATFORM))))
	$(eval IMAGE_PLAT := $(subst _,/,$(PLATFORM)))
	$(eval IMAGE_NAME := $(REGISTRY_PREFIX)/$(IMAGE)-$(ARCH):$(VERSION))
	@echo "===========> Building docker image $(IMAGE) $(VERSION) for $(IMAGE_PLAT)"
	$(DOCKER) buildx build --platform $(IMAGE_PLAT) --load -t $(IMAGE_NAME) $(_DOCKER_BUILD_EXTRA_ARGS) \
	 -f $(ROOT_DIR)/build/docker/Dockerfile_arch $(ROOT_DIR)

.PHONY: image.push
image.push: image.buildx.verify $(addprefix image.push., $(addprefix $(PLATFORM)., $(BINS)))

.PHONY: image.push.multiarch
image.push.multiarch: image.buildx.verify $(foreach p,$(PLATFORMS),$(addprefix image.push., $(addprefix $(p)., $(BINS))))

.PHONY: image.push.%
image.push.%: image.build.%
	@echo "===========> Pushing image $(IMAGE) $(VERSION) to $(REGISTRY_PREFIX)"
	$(DOCKER) push $(REGISTRY_PREFIX)/$(IMAGE)-$(ARCH):$(VERSION)

.PHONY: image.manifest.push
image.manifest.push: export DOCKER_CLI_EXPERIMENTAL := enabled
image.manifest.push: image.buildx.verify $(addprefix image.manifest.push., $(addprefix $(PLATFORM)., $(BINS)))

.PHONY: image.manifest.push.%
image.manifest.push.%: image.push.% image.manifest.remove.%
	@echo "===========> Pushing manifest $(IMAGE) $(VERSION) to $(REGISTRY_PREFIX) and then remove the local manifest list"
	@$(DOCKER) manifest create $(REGISTRY_PREFIX)/$(IMAGE):$(VERSION) \
		$(REGISTRY_PREFIX)/$(IMAGE)-$(ARCH):$(VERSION)
	@$(DOCKER) manifest annotate $(REGISTRY_PREFIX)/$(IMAGE):$(VERSION) \
		$(REGISTRY_PREFIX)/$(IMAGE)-$(ARCH):$(VERSION) \
		--os $(OS) --arch ${ARCH}
	@$(DOCKER) manifest push --purge $(REGISTRY_PREFIX)/$(IMAGE):$(VERSION)

# Docker cli has a bug: https://github.com/docker/cli/issues/954
# If you find your manifests were not updated,
# Please manually delete them in $HOME/.docker/manifests/
# and re-run.
.PHONY: image.manifest.remove.%
image.manifest.remove.%:
	@rm -rf ${HOME}/.docker/manifests/docker.io_$(REGISTRY_PREFIX)_$(IMAGE)-$(VERSION)

.PHONY: image.manifest.push.multiarch
image.manifest.push.multiarch: image.push.multiarch $(addprefix image.manifest.push.multiarch., $(BINS))

.PHONY: image.manifest.push.multiarch.%
image.manifest.push.multiarch.%:
	@echo "===========> Pushing manifest $* $(VERSION) to $(REGISTRY_PREFIX) and then remove the local manifest list"
	REGISTRY_PREFIX=$(REGISTRY_PREFIX) PLATFROMS="$(PLATFORMS)" IMAGE=$* VERSION=$(VERSION) DOCKER_CLI_EXPERIMENTAL=enabled \
	$(ROOT_DIR)/build/lib/create-manifest.sh 
