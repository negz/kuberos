FROM alpine:3.7
MAINTAINER Nic Cope <n+docker@rk0n.org>

RUN apk update && apk add ca-certificates

COPY "dist/kuberos" /