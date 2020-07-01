#!/usr/bin/env bash

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

set -o errexit
set -o nounset
set -o pipefail

BUILDX_VERSION=${BUILDX_VERSION:-"v0.4.1"}
ARCH=${ARCH:-"amd64"}
BUILDX_BIN="https://github.com/docker/buildx/releases/download/${BUILDX_VERSION}/buildx-${BUILDX_VERSION}.linux-${ARCH}"

echo "Downloading docker-buildx"
wget -c ${BUILDX_BIN} -O ./docker-buildx
mkdir -p ~/.docker/cli-plugins/
mv ./docker-buildx ~/.docker/cli-plugins/
chmod a+x ~/.docker/cli-plugins/docker-buildx
docker buildx version
