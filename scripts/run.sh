#!/usr/bin/env bash

set -ex

OIDC_CLIENT_ID=$1
OIDC_CLIENT_SECRET=$2

CFG=$(mktemp -d /tmp/kuberos.XXXX)
echo $OIDC_CLIENT_SECRET >$CFG/secret
cat <<EOF >$CFG/template
apiVersion: v1
kind: Config
clusters:
- name: kuberos
  cluster:
    certificate-authority-data: REDACTED
    server: https://kuberos.example.org
EOF

VERSION=$(git rev-parse --short HEAD)
NAME=kuberos

docker kill ${NAME} || true
docker rm ${NAME} || true

docker run -d \
	--name ${NAME} \
	-p 10003:10003 \
	-v $CFG:/cfg \
	"negz/kuberos:${VERSION}" /kuberos \
	https://accounts.google.com \
	$OIDC_CLIENT_ID \
	/cfg/secret \
	/cfg/template
