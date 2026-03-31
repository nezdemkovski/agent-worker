FROM golang:1.26.1-bookworm AS go-toolchain

FROM node:24.14.0-bookworm-slim

ARG KUBECTL_VERSION=v1.35.3
ARG PNPM_VERSION=10.27.0
ARG MIRRORD_VERSION=3.195.0

ENV GOROOT=/usr/local/go \
    GOPATH=/go \
    PATH=/usr/local/go/bin:/go/bin:${PATH}

COPY --from=go-toolchain /usr/local/go /usr/local/go

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        git \
        jq \
        openssh-client \
    && ARCH="$(dpkg --print-architecture)" \
    && case "${ARCH}" in \
         amd64) MIRRORD_ARCH="x86_64" ;; \
         arm64) MIRRORD_ARCH="aarch64" ;; \
         *) echo "unsupported architecture: ${ARCH}" >&2; exit 1 ;; \
       esac \
    && curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl" -o /usr/local/bin/kubectl \
    && chmod +x /usr/local/bin/kubectl \
    && curl -fsSL "https://github.com/metalbear-co/mirrord/releases/download/${MIRRORD_VERSION}/mirrord_linux_${MIRRORD_ARCH}" -o /usr/local/bin/mirrord \
    && chmod +x /usr/local/bin/mirrord \
    && corepack enable \
    && corepack prepare pnpm@${PNPM_VERSION} --activate \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace
