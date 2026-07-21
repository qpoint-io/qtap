#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf '%s\n' "Usage: ${0##*/} [--version VERSION] [--commit SHA] [--ref REF] [--build-time RFC3339] [--output-dir DIR]" >&2
}

version=
commit=
ref=
build_time=
output_dir=dist
while (($#)); do
  case "$1" in
    --version) version=${2-}; shift 2 ;;
    --commit) commit=${2-}; shift 2 ;;
    --ref) ref=${2-}; shift 2 ;;
    --build-time) build_time=${2-}; shift 2 ;;
    --output-dir) output_dir=${2-}; shift 2 ;;
    *) usage; exit 2 ;;
  esac
done

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd -- "$repo_root"
git rev-parse --is-inside-work-tree >/dev/null

if [[ -n $(git status --porcelain) && ( -z "$version" || -z "$commit" || -z "$ref" || -z "$build_time" ) ]]; then
  printf 'Dirty checkouts require explicit --version, --commit, --ref, and --build-time metadata\n' >&2
  exit 1
fi

if [[ -z "$version" ]]; then
  version=$(git describe --tags --always)
fi
if [[ -z "$commit" ]]; then
  commit=$(git rev-parse HEAD)
fi
if [[ -z "$ref" ]]; then
  if tag=$(git describe --tags --exact-match 2>/dev/null); then
    ref="refs/tags/$tag"
  elif branch=$(git symbolic-ref -q HEAD); then
    ref=$branch
  else
    printf 'Detached checkouts without an exact tag require --ref\n' >&2
    exit 1
  fi
fi
if [[ -z "$build_time" ]]; then
  timestamp=$(git show -s --format=%ct "$commit")
  build_time=$(date -u -d "@$timestamp" '+%Y-%m-%dT%H:%M:%SZ')
fi

[[ "$commit" =~ ^[0-9a-f]{40,64}$ ]] || {
  printf 'Commit must be a full hexadecimal object ID\n' >&2
  exit 1
}
[[ $(git rev-parse HEAD) == "$commit" ]] || {
  printf 'Commit metadata does not match the checked-out revision\n' >&2
  exit 1
}
[[ "$ref" == refs/* ]] || {
  printf 'Ref must be a full refs/... name\n' >&2
  exit 1
}
if git rev-parse --verify --quiet "${ref}^{commit}" >/dev/null; then
  [[ $(git rev-parse "${ref}^{commit}") == "$commit" ]] || {
    printf 'Ref metadata does not resolve to the checked-out revision\n' >&2
    exit 1
  }
fi
[[ "$build_time" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] || {
  printf 'Build time must be RFC3339 UTC\n' >&2
  exit 1
}

work_dir=$(mktemp -d)
trap 'rm -rf -- "$work_dir"' EXIT
make \
  BIN_DIR="$work_dir/bin" \
  VERSION="$version" \
  GIT_COMMIT="$commit" \
  GIT_REF="$ref" \
  BUILD_TIME="$build_time" \
  GOOS=linux \
  GOARCH=amd64 \
  CGO_ENABLED=0 \
  build-binary

archive=$(bash scripts/release/package-archive.sh \
  --binary "$work_dir/bin/qtap" \
  --output-dir "$output_dir")
bash scripts/release/verify-archive.sh \
  --archive "$archive" \
  --checksums "$(dirname -- "$archive")/SHA256SUMS" \
  --version "$version" \
  --commit "$commit" \
  --ref "$ref" \
  --build-time "$build_time"
