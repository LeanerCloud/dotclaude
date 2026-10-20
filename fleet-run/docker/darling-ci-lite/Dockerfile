# Lightweight Linux checks for Darling (CI scripts, unit tests, lint, small cmake configure probes).
# Not for full Darling builds. Base matches the darling workflows' runners (ubuntu-24.04 / ubuntu-latest).
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONDONTWRITEBYTECODE=1 \
    LC_ALL=C.UTF-8

# Throwaway CI image: apt pins would rot with every Ubuntu update.
# hadolint ignore=DL3008
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      bash ca-certificates git python3 python3-yaml \
      tar zstd xz-utils binutils file \
      shellcheck cmake ninja-build make pkg-config clang \
 && rm -rf /var/lib/apt/lists/*

# ubuntu:24.04 ships user 'ubuntu' (uid 1000); run checks as it.
USER 1000
WORKDIR /src
