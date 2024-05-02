# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

################################################################################
##                               BUILD ARGS                                   ##
################################################################################
# This build arg allows the specification of a custom Golang image.
ARG GOLANG_IMAGE=golang:1.22.2

# The distroless image on which the CPI manager image is built.
#
# Please do not use "latest". Explicit tags should be used to provide
# deterministic builds. Follow what kubernetes uses to build
# kube-controller-manager, for example for 1.27.x:
# https://github.com/kubernetes/kubernetes/blob/release-1.27/build/common.sh#L99
ARG DISTROLESS_IMAGE=registry.k8s.io/build-image/go-runner:v2.3.1-go1.22.2-bookworm.0

# We use Alpine as the source for default CA certificates and some output
# images
ARG ALPINE_IMAGE=alpine:3.17.5

# vngcloud-blockstorage-csi-driver uses Debian as a base image
ARG DEBIAN_IMAGE=registry.k8s.io/build-image/debian-base:bullseye-v1.4.3

################################################################################
##                              BUILD STAGE                                   ##
################################################################################

# Build an image containing a common ca-certificates used by all target images
# regardless of how they are built. We arbitrarily take ca-certificates from
# the amd64 Alpine image.
FROM --platform=linux/amd64 ${ALPINE_IMAGE} as certs
RUN apk add --no-cache ca-certificates


# Build all command targets. We build all command targets in a single build
# stage for efficiency. Target images copy their binary from this image.
# We use go's native cross compilation for multi-arch in this stage, so the
# builder itself is always amd64
FROM --platform=linux/amd64 ${GOLANG_IMAGE} as builder

ARG GOPROXY=https://goproxy.io,direct
ARG TARGETOS
ARG TARGETARCH
ARG VERSION

WORKDIR /build
COPY Makefile go.mod go.sum ./
COPY cmd/ cmd/
COPY pkg/ pkg/
RUN make build GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOPROXY=${GOPROXY} VERSION=${VERSION}


################################################################################
##                             TARGET IMAGES                                  ##
################################################################################


##
## vngcloud-blockstorage-csi-driver
##

# step 1: copy all necessary files from Debian distro to /dest folder
# all magic happens in tools/csi-deps.sh
FROM --platform=${TARGETPLATFORM} ${DEBIAN_IMAGE} as vngcloud-blockstorage-csi-driver-utils

RUN clean-install bash rsync mount udev btrfs-progs e2fsprogs xfsprogs util-linux
COPY tools/csi-deps.sh /tools/csi-deps.sh
RUN /tools/csi-deps.sh

# step 2: check if all necessary files are copied and work properly
# the build have to finish without errors, but the result image will not be used
FROM --platform=${TARGETPLATFORM} ${DISTROLESS_IMAGE} as vngcloud-blockstorage-csi-driver-utils-check

COPY --from=vngcloud-blockstorage-csi-driver-utils /dest /
COPY --from=vngcloud-blockstorage-csi-driver-utils /bin/sh /bin/sh
COPY tools/csi-deps-check.sh /tools/csi-deps-check.sh

SHELL ["/bin/sh"]
RUN /tools/csi-deps-check.sh

# step 3: build tiny vngcloud-blockstorage-csi-driver image with only necessary files
FROM --platform=${TARGETPLATFORM} ${DISTROLESS_IMAGE} as vngcloud-blockstorage-csi-driver

# Copying csi-deps-check.sh simply ensures that the resulting image has a dependency
# on vngcloud-blockstorage-csi-driver-utils-check and therefore that the check has passed
COPY --from=vngcloud-blockstorage-csi-driver-utils-check /tools/csi-deps-check.sh /bin/csi-deps-check.sh
COPY --from=vngcloud-blockstorage-csi-driver-utils /dest /
COPY --from=builder /build/vngcloud-blockstorage-csi-driver /bin/vngcloud-blockstorage-csi-driver
COPY --from=certs /etc/ssl/certs /etc/ssl/certs

LABEL name="vngcloud-blockstorage-csi-driver" \
      maintainers="Cuong. Duong Manh" \
      email="cuongdm3@vng.com.vn" \
      description="VngCloud BlockStorage CSI driver for VKS"

CMD ["/bin/vngcloud-blockstorage-csi-driver"]
