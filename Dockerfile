# Use build arguments to define the platform specifics
# Go version from https://hub.docker.com/_/golang
ARG GO_VERSION=1.26.5
# Node.js LTS version
ARG NODE_MAJOR=24
ARG GIT_VERSION=dev
ARG GIT_COMMIT=unknown
ARG GIT_REF=unknown
ARG BUILD_TIME=unknown
ARG SOURCE_URL=https://github.com/qpoint-io/qtap
ARG LICENSE=AGPL-3.0-only

# ==================================
# BASE IMAGE
# ==================================
# Use Debian Bookworm
FROM golang:${GO_VERSION}-bookworm AS base

ENV GOCACHE=/root/.cache/go-build

# Install dependencies - grouped into one RUN to reduce layers
ARG NODE_MAJOR
ARG TARGETARCH
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl gnupg && \
    mkdir -p /etc/apt/keyrings && \
    curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg && \
    echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_${NODE_MAJOR}.x nodistro main" | tee /etc/apt/sources.list.d/nodesource.list && \
    apt-get update && \
    apt-get install -y --no-install-recommends \
    ca-certificates \
    build-essential \
    pkg-config \
    clang-14 \
    clang-format \
    llvm-14 \
    libelf-dev \
    linux-headers-${TARGETARCH} \
    linux-libc-dev \
    openjdk-17-jdk-headless \
    nodejs \
    jq \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/include/asm-generic/ /usr/include/asm \
    && mkdir /sources/

# Used to build libbpf and bpftool
WORKDIR /sources/

# Install bpftool
ARG BPFTOOL_VERSION=v7.5.0
RUN --mount=type=cache,target=/root/.cache/bpftool \
    git clone --branch ${BPFTOOL_VERSION} --depth 1 https://github.com/libbpf/bpftool.git && \
    cd bpftool && \
    git submodule update --init && \
    make -C src/ install && \
    cd ..

# Handle Go dependencies separately - will only rebuild if go.mod/go.sum change
WORKDIR /app
COPY go.mod go.sum ./

# install go modules
RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=bind,source=go.mod,target=go.mod \
    go mod download -x

# Reset WORKDIR to /sources for consistency
WORKDIR /sources/

# ==================================
# DEVELOPMENT IMAGE
# ==================================
FROM base AS dev

ENV ENVIRONMENT=dev

# Combine RUN commands and use specific versions for packages
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get -o Acquire::http::Timeout=30 \
    -o Acquire::ftp::Timeout=30 \
    -o Acquire::Retries=1 \
    update && \
    apt-get install -y --no-install-recommends \
    strace \
    bpftrace \
    linux-perf \
    curl \
    net-tools \
    iproute2 \
    vim \
    man \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    && git config --global --add safe.directory /qtap

# Install newest docker client
RUN curl -fsSL https://get.docker.com | sh

# Install FlameGraph tools for perf profiling
RUN git clone --depth 1 https://github.com/brendangregg/FlameGraph.git /tmp/FlameGraph && \
    install -m 755 /tmp/FlameGraph/flamegraph.pl /usr/local/bin/flamegraph.pl && \
    install -m 755 /tmp/FlameGraph/stackcollapse-perf.pl /usr/local/bin/stackcollapse-perf.pl && \
    install -m 755 /tmp/FlameGraph/difffolded.pl /usr/local/bin/difffolded.pl && \
    rm -rf /tmp/FlameGraph

# ==================================
# CI IMAGE
# ==================================
FROM base AS ci

ENV ENVIRONMENT=prod

WORKDIR /src
COPY . .

# CI validates the source separately. This stage builds the exact source snapshot
# without a writable bind back to the host checkout.
RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=cache,target=/root/.cache/go-build \
    make generate

ARG GIT_VERSION
ARG GIT_COMMIT
ARG GIT_REF
ARG BUILD_TIME

RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=cache,target=/root/.cache/go-build \
    make \
      VERSION="${GIT_VERSION}" \
      GIT_COMMIT="${GIT_COMMIT}" \
      GIT_REF="${GIT_REF}" \
      BUILD_TIME="${BUILD_TIME}" \
      build-binary && \
    mkdir -p /app/dist && \
    cp bin/qtap /app/dist/qtap

CMD ["/bin/sh"]

# ==================================
# PRODUCTION IMAGE
# ==================================
FROM debian:bookworm-slim AS prod

# Combine RUN commands and use specific versions
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get -o Acquire::http::Timeout=30 \
    -o Acquire::ftp::Timeout=30 \
    -o Acquire::Retries=1 \
    update && \
    apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    net-tools \
    tini \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    && addgroup --gid 1010 qtap \
    && adduser --uid 1010 --gid 1010 --disabled-password --gecos '' qtap \
    && ln -sf /proc/1/fd/1 /dev/stdout \
    && ln -sf /proc/1/fd/2 /dev/stderr \
    && mkdir /app \
    && chown qtap:qtap /app

ARG GIT_VERSION
ARG GIT_COMMIT
ARG GIT_REF
ARG BUILD_TIME
ARG SOURCE_URL
ARG LICENSE

LABEL org.opencontainers.image.title="qtap" \
    org.opencontainers.image.description="An eBPF agent that captures pre-encrypted network traffic, providing rich context about egress connections and their originating processes." \
    org.opencontainers.image.source="${SOURCE_URL}" \
    org.opencontainers.image.url="${SOURCE_URL}" \
    org.opencontainers.image.licenses="${LICENSE}" \
    org.opencontainers.image.version="${GIT_VERSION}" \
    org.opencontainers.image.revision="${GIT_COMMIT}" \
    org.opencontainers.image.ref.name="${GIT_REF}" \
    org.opencontainers.image.created="${BUILD_TIME}"

WORKDIR /app
USER 0:0

# Copy only the binary
COPY --from=ci /app/dist/qtap /usr/local/bin/qtap

# Set the entrypoint to run the binary when the container starts
ENTRYPOINT ["tini", "--", "qtap"]
