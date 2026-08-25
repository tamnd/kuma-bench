# The image the kuma runner executes in.
#
# Pinned by digest for the same reason the Python image is: the toolchain and
# the C library underneath it both affect the numbers, and a tag does not pin
# either of them.
FROM golang@sha256:a9278c7936a41f7d33dae94784df33442e71fb4d6943a08100cf1898845c5bea

# GOTOOLCHAIN=local stops Go from downloading a different toolchain than the
# one in this image. Without it, a go.mod bump would silently change the
# compiler a benchmark ran on, which is exactly the thing pinning is for.
ENV GOTOOLCHAIN=local \
    GOFLAGS=-mod=mod \
    CGO_ENABLED=0

# The module cache is warmed at build time so the timed section never waits on
# a download. The repository is mounted read only at run time, so this has to
# happen now or not at all.
WORKDIR /build
COPY suites/dbbench/kuma/go.mod suites/dbbench/kuma/go.sum ./
RUN go mod download

WORKDIR /work
ENTRYPOINT []
