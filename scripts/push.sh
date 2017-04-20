#!/usr/bin/env sh

set -e

# Create the docker image
VERSION=$(git rev-parse --short HEAD)
docker push "negz/kuberos:latest"
docker push "negz/kuberos:${VERSION}"

