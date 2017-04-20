FROM alpine:3.5
MAINTAINER Nic Cope <n+docker@rk0n.org>

ENV APP /kuberos

RUN mkdir -p "${APP}/frontend/dist"
COPY "frontend/dist" "${APP}/frontend/dist"
COPY "frontend/index.html" "${APP}/frontend/"

COPY "dist/kuberos" "${APP}"