FROM golang:1.26.1-bookworm AS go-toolchain

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/dockhand ./cmd/dockhand

FROM node:24.14.0-bookworm-slim

ARG KUBECTL_VERSION=v1.35.3
ARG PNPM_VERSION=10.27.0
ARG MIRRORD_VERSION=3.195.0
ARG AIR_VERSION=v1.64.5

ENV GOROOT=/usr/local/go \
    GOPATH=/go \
    PATH=/usr/local/go/bin:/go/bin:${PATH}

COPY --from=go-toolchain /usr/local/go /usr/local/go
COPY --from=go-toolchain /out/dockhand /usr/local/bin/dockhand

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
    && GOBIN=/usr/local/bin go install github.com/air-verse/air@${AIR_VERSION} \
    && npm install -g @anthropic-ai/claude-code@latest \
    && case "${ARCH}" in \
         amd64) CODEX_ARCH="x86_64" ;; \
         arm64) CODEX_ARCH="aarch64" ;; \
         *) echo "unsupported architecture: ${ARCH}" >&2; exit 1 ;; \
       esac \
    && curl -fsSL "https://github.com/openai/codex/releases/latest/download/codex-${CODEX_ARCH}-unknown-linux-musl.tar.gz" \
       | tar -xz -C /usr/local/bin \
    && mv /usr/local/bin/codex-${CODEX_ARCH}-unknown-linux-musl /usr/local/bin/codex \
    && chmod +x /usr/local/bin/codex \
    && corepack enable \
    && corepack prepare pnpm@${PNPM_VERSION} --activate \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace
