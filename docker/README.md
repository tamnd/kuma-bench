# Pinned images

Two images, one per language. They exist so that a result from six months ago can be reproduced today, which means everything in them is pinned by digest or exact version and nothing resolves to latest.

Build both:

```
docker/build.sh
```

Then run the suite against them:

```
go run ./cmd/kumabench -docker -suite dbbench -size 0.5GB
```

The orchestrator mounts the repository read only at `/work` and the datasets read only at `/data`. Read only for both, because a runner that writes into the data directory has changed the input for whatever runs next.

## Why pin by digest

A tag is a moving target. `python:3.13-slim` was a different image last month and will be a different one next month, and the difference includes the glibc version, which changes how fast a hash table is. Pinning the tag is not enough and pinning the digest is, so the digest is what is written down.

Updating a digest is a commit of its own with the reason in the message. If the numbers move after one of those, the commit is right there in the history next to the jump.

## What is not pinned

The kernel and the CPU. Nothing inside a container can pin those, which is why every result record carries the machine description with it and why numbers from different machines are never put in the same table.
