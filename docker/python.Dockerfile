# The image the pandas and polars runners execute in.
#
# The base is pinned by digest rather than by tag. python:3.13-slim-bookworm
# points at a different image every few weeks, and the difference includes the
# glibc version, which is enough to move a hash table benchmark. Updating this
# digest is a commit of its own, so that a jump in the numbers can be read
# against the commit that caused it.
FROM python@sha256:8edbf9e42c7fb168b9c523718ed907117e6d2e60f5889c0c499bbda3a787da53

# uv resolves and installs from the lock file, which is what actually pins
# pandas and Polars. The image only pins the interpreter under them.
COPY --from=ghcr.io/astral-sh/uv:0.12.5 /uv /usr/local/bin/uv

ENV UV_PROJECT_ENVIRONMENT=/opt/venv \
    UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    PATH=/opt/venv/bin:$PATH

WORKDIR /build
COPY pyproject.toml uv.lock ./

# --frozen fails rather than re-resolving if the lock file does not match the
# project file. A benchmark image that quietly installed a different version
# than the one recorded would make every result in it a lie.
RUN uv sync --frozen --no-dev --no-install-project

# The repository is mounted at /work when this runs, so nothing else is copied
# in. That keeps the image the same across commits, which means it is built
# once and reused rather than rebuilt on every run.
WORKDIR /work
ENTRYPOINT []
