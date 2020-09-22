#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

ROOT=$(cd $(dirname "${BASH_SOURCE}")/.. && pwd -P)
GIT_VERSION_FILE="${ROOT}/.version-defs"

source "${ROOT}/hack/lib/version.sh"
source "${ROOT}/hack/lib/golang.sh"

if [[ -f ${GIT_VERSION_FILE} ]]; then
  api::version::load_version_vars "${GIT_VERSION_FILE}"
else
  api::version::get_version_vars
  api::version::save_version_vars "${GIT_VERSION_FILE}"
fi

cd $ROOT
CGO_ENABLED=0 go build -o bin/tapp-controller -ldflags "$(api::version::ldflags)" .
if [ $? -eq 0 ]; then
  echo "Build success!"
else
  echo "Faild to build!"
fi
