# Sandbox runtime image: a base environment with common tooling.
# Phase 1 keeps tooling fat; later phases move to minimal per-template images.
FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
        git ca-certificates curl wget \
        python3 python3-pip \
        build-essential \
    && rm -rf /var/lib/apt/lists/*

# Go toolchain
ENV GOLANG_VERSION=1.25.6
RUN wget -q https://go.dev/dl/go${GOLANG_VERSION}.linux-amd64.tar.gz -O /tmp/go.tgz \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm /tmp/go.tgz
ENV PATH="/usr/local/go/bin:${PATH}"

# Node toolchain
ENV NODE_VERSION=20.19.0
RUN wget -q https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz -O /tmp/node.tar.xz \
    && tar -C /usr/local -xJf /tmp/node.tar.xz \
    && ln -sf /usr/local/node-v${NODE_VERSION}-linux-x64/bin/node /usr/local/bin/node \
    && ln -sf /usr/local/node-v${NODE_VERSION}-linux-x64/bin/npm /usr/local/bin/npm \
    && rm /tmp/node.tar.xz

WORKDIR /repo
