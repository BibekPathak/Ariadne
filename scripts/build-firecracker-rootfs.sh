#!/usr/bin/env bash
# Builds deploy/firecracker/rootfs.ext4: an ext4 image of the kubeai-sandbox
# Docker image with the sandbox-agent baked in as PID 1 (/sandbox-agent).
#
# Prerequisites: the kubeai-sandbox image (make sandbox-image), docker,
# mke2fs + debugfs (e2fsprogs) on the host.
set -euo pipefail

cd "$(dirname "$0")/.."
OUT=deploy/firecracker
IMG=kubeai-sandbox:local
SIZE_BLOCKS=${FIRECRACKER_ROOTFS_BLOCKS:-270000} # ~1.05 GiB @ 4k

echo "==> building sandbox-agent"
CGO_ENABLED=0 go build -o /tmp/kubeai-sandbox-agent ./cmd/sandbox-agent

echo "==> exporting $IMG"
rm -rf /tmp/kubeai-rootfs && mkdir -p /tmp/kubeai-rootfs
CID=$(docker create "$IMG")
docker export "$CID" | tar -x -C /tmp/kubeai-rootfs --no-same-owner --no-same-permissions
docker rm "$CID" >/dev/null

echo "==> creating ext4 image"
rm -f "$OUT/rootfs.ext4"
mke2fs -q -t ext4 -d /tmp/kubeai-rootfs -b 4096 -L kubeai-rootfs "$OUT/rootfs.ext4" "$SIZE_BLOCKS"

echo "==> baking sandbox-agent into image"
debugfs -w -R "write /tmp/kubeai-sandbox-agent /sandbox-agent" "$OUT/rootfs.ext4" >/dev/null 2>&1

echo "==> done: $OUT/rootfs.ext4"
ls -lh "$OUT/rootfs.ext4"
