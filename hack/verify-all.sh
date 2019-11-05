#!/bin/bash

# GoFmt apparently is changing @ head...

set -o errexit
set -o nounset
set -o pipefail

ROOT=$(cd $(dirname "${BASH_SOURCE}")/.. && pwd -P)

# verify gofmt
${ROOT}/hack/verify-gofmt.sh

# verify codegen
${ROOT}/hack/verify-codegen.sh
