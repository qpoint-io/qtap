#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'Usage: %s --binary PATH --output-dir DIR\n' "${0##*/}" >&2
}

binary=
output_dir=
while (($#)); do
  case "$1" in
    --binary)
      binary=${2-}
      shift 2
      ;;
    --output-dir)
      output_dir=${2-}
      shift 2
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

if [[ -z "$binary" || -z "$output_dir" || ! -x "$binary" ]]; then
  usage
  exit 2
fi

for command in date file jq sha256sum tar; do
  command -v "$command" >/dev/null || {
    printf 'Required command not found: %s\n' "$command" >&2
    exit 1
  }
done

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
[[ -f "$repo_root/LICENSE" ]] || {
  printf 'LICENSE not found at repository root\n' >&2
  exit 1
}

file "$binary" | grep -Eq 'ELF 64-bit LSB.*(x86-64|x86_64)' || {
  printf 'Binary is not a Linux AMD64 ELF: %s\n' "$binary" >&2
  exit 1
}

metadata=$("$binary" build-info)
jq -e '
  .version != "dev" and .version != "unknown" and (.version | endswith("-dirty") | not) and
  (.commit | test("^[0-9a-f]{40,64}$")) and
  (.ref | startswith("refs/")) and .build_time != "unknown" and
  .source == "https://github.com/qpoint-io/qtap" and
  .license == "AGPL-3.0-only" and
  .os == "linux" and .architecture == "amd64"
' <<<"$metadata" >/dev/null

version=$(jq -er '.version' <<<"$metadata")
build_time=$(jq -er '.build_time' <<<"$metadata")
[[ "$version" =~ ^v?[0-9A-Za-z][0-9A-Za-z._-]*$ ]] || {
  printf 'Unsafe release version: %s\n' "$version" >&2
  exit 1
}
[[ "$build_time" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] || {
  printf 'Build time must be RFC3339 UTC: %s\n' "$build_time" >&2
  exit 1
}
[[ $(date -u -d "$build_time" '+%Y-%m-%dT%H:%M:%SZ') == "$build_time" ]] || {
  printf 'Build time is not a valid RFC3339 UTC timestamp: %s\n' "$build_time" >&2
  exit 1
}

version_output=$("$binary" --version)
grep -F -- "$version" <<<"$version_output" >/dev/null || {
  printf 'Binary --version output does not contain %s\n' "$version" >&2
  exit 1
}

mkdir -p -- "$output_dir"
output_dir=$(cd -- "$output_dir" && pwd)
archive_name="qtap-${version}-linux-amd64.tgz"
archive="$output_dir/$archive_name"
stage=$(mktemp -d)
trap 'rm -rf -- "$stage"' EXIT

install -m 0755 -- "$binary" "$stage/qtap"
install -m 0644 -- "$repo_root/LICENSE" "$stage/LICENSE"
jq -S . <<<"$metadata" >"$stage/release-metadata.json"
chmod 0644 "$stage/release-metadata.json"

tar \
  --sort=name \
  --owner=0 \
  --group=0 \
  --numeric-owner \
  --mtime="$build_time" \
  -czf "$archive" \
  -C "$stage" \
  LICENSE qtap release-metadata.json

(
  cd -- "$output_dir"
  sha256sum -- "$archive_name" >SHA256SUMS
)

printf '%s\n' "$archive"
