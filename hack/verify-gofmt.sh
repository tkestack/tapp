#!/bin/bash

# GoFmt apparently is changing @ head...

set -o errexit
set -o nounset
set -o pipefail

ROOT=$(cd $(dirname "${BASH_SOURCE}")/.. && pwd -P)
source $ROOT/hack/lib/lib.sh

GOFMT="gofmt -s"
bad_files=$(find_files | xargs $GOFMT -l)
if [[ -n "${bad_files}" ]]; then
  echo "!!! '$GOFMT -w' needs to be run on the following files: "
  echo "${bad_files}"
  echo "run 'git ls-files -m | grep .go | xargs -n1 gofmt -s -w' to format your own code"
  exit 1
fi

go build -o /tmp/import_checker ${ROOT}/hack/import_checker.go

(
  export PACKAGE_NAME="tkestack.io/tapp"
  /tmp/import_checker $(find_files)
)
