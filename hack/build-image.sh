#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

ROOT=$(cd $(dirname "${BASH_SOURCE}")/.. && pwd -P)
GIT_VERSION_FILE="${ROOT}/.version-defs"
BIN="build/docker/tapp-controller"

source "${ROOT}/hack/lib/version.sh"
source "${ROOT}/hack/lib/golang.sh"

if [[ -f ${GIT_VERSION_FILE} ]]; then
  api::version::load_version_vars "${GIT_VERSION_FILE}"
else
  api::version::get_version_vars
  api::version::save_version_vars "${GIT_VERSION_FILE}"
fi

go get github.com/mitchellh/gox

cd $GOPATH/src/$API_GO_PACKAGE/tapp-controller
CGO_ENABLED=0 gox -osarch="linux/amd64" -ldflags "$(api::version::ldflags)" \
	-output=$BIN
docker build build/docker/ -t tappcontroller:v1.0.0
rm $BIN