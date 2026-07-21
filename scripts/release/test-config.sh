#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd -- "$repo_root"

[[ $(grep -El 'GAR_JSON_KEY|gh release create|aws s3 cp' .github/workflows/*.yaml | wc -l) -eq 1 ]]
grep -F "DOCKER_TAG   ?= \$(VERSION)" Makefile >/dev/null
grep -F "org.opencontainers.image.ref.name=\"\${GIT_REF}\"" Dockerfile >/dev/null
grep -F "org.opencontainers.image.source=\"\${SOURCE_URL}\"" Dockerfile >/dev/null
grep -F "org.opencontainers.image.licenses=\"\${LICENSE}\"" Dockerfile >/dev/null
grep -F 'if: github.event_name == '\''push'\''' .github/workflows/release.yaml >/dev/null
grep -F 'Immutable image tag already exists' .github/workflows/release.yaml >/dev/null
grep -F 'imagetools create --prefer-index=false' .github/workflows/release.yaml >/dev/null
grep -F 's3://downloads/qpoint/qtap-latest-linux-amd64.tgz' .github/workflows/release.yaml >/dev/null
grep -F "highest_release=\$(git tag --list 'v*'" .github/workflows/release.yaml >/dev/null
if grep -Eq '(^|[,=[:space:]])latest($|[,[:space:]])' .github/workflows/release.yaml; then
  printf 'Release workflow must not publish a latest image tag\n' >&2
  exit 1
fi

printf 'Release configuration invariants verified\n'
