#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

ROOT=$(cd $(dirname "${BASH_SOURCE}")/.. && pwd -P)

source "${ROOT}/hack/lib/golang.sh"

TIMEOUT=${TIMEOUT:-5m}

cd $ROOT

packages=$(find . -name *_test.go -print0 | xargs -0n1 dirname | sed 's|^\./||' | sort -u | grep -v e2e)
for i in $packages
do
  echo tkestack.io/tapp/$i
  go test -v -cover -test.timeout=${TIMEOUT}  tkestack.io/tapp/$i
done
cd -
