# Copyright 2021 Synology Inc.

############## Build stage ##############
# The builder runs on the build machine's own platform and cross-compiles.
# The Makefile builds a static binary with CGO_ENABLED=0, and the RUN below
# sets GOARCH and GOARM from TARGETPLATFORM, so no emulator runs the compiler.
# Each base image is pinned to a digest, because a tag can be pushed again
# with other contents. Dependabot moves the tag and the digest together.
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS builder
LABEL stage=synobuilder

RUN apk add --no-cache alpine-sdk
WORKDIR /go/src/synok8scsiplugin
COPY go.mod go.sum ./
RUN go mod download

COPY Makefile .

ARG TARGETPLATFORM

COPY main.go .
COPY pkg ./pkg
COPY synocli ./synocli
RUN env GOARCH=$(echo "$TARGETPLATFORM" | cut -f2 -d/) \
        GOARM=$(echo "$TARGETPLATFORM" | cut -f3 -d/ | cut -c2-) \
        make

############## Final stage ##############
FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

ARG IMAGE_VERSION=dev
ARG IMAGE_RELEASE=1
LABEL name="synology-csi" \
      maintainer="Synology" \
      vendor="Synology Inc." \
      version="${IMAGE_VERSION}" \
      release="${IMAGE_RELEASE}" \
      summary="Synology CSI driver for Kubernetes" \
      description="A Container Storage Interface (CSI) driver for Synology NAS."

# Runtime tools. mkfs, fsck, and resize run inside this container, so these
# packages decide which fsType values a StorageClass can use. Alpine publishes
# every one of them, so the image builds with no RHEL subscription, and
# btrfs-progs keeps fsType btrfs working.
RUN apk upgrade --no-cache && apk add --no-cache e2fsprogs e2fsprogs-extra xfsprogs xfsprogs-extra blkid \
        util-linux iproute2 bash btrfs-progs ca-certificates cifs-utils nfs-utils nvme-cli

COPY LICENSE /licenses/LICENSE

WORKDIR /

# Copy and run CSI driver
COPY --from=builder /go/src/synok8scsiplugin/bin/synology-csi-driver synology-csi-driver

# Declare a non-root user to satisfy the RunAsNonRoot certification check.
# The node DaemonSet still runs privileged with runAsUser: 0 (mount / iscsiadm
# need root); the controller Deployment can run as this user.
USER 1000

ENTRYPOINT ["/synology-csi-driver"]
