#!/bin/sh
# Build the two runner images.
#
# Run from anywhere. The build context is the repository root, because the
# Python image needs the lock file and the Go image needs the runner's go.mod.
set -eu

# CDPATH is cleared for the length of the cd so that a CDPATH set in the
# caller's environment cannot send it somewhere else. shellcheck reads the
# empty assignment as a mistake, and it is not.
# shellcheck disable=SC1007
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

docker build -f "$root/docker/python.Dockerfile" -t kuma-bench-python "$root"
docker build -f "$root/docker/go.Dockerfile" -t kuma-bench-go "$root"

echo
echo "built kuma-bench-python and kuma-bench-go"
echo "run: go run ./cmd/kumabench -docker -suite dbbench -size 0.5GB"
