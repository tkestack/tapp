#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

IMAGE=$1
docker push $IMAGE
