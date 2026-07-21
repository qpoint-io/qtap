#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf '%s\n' "Usage: ${0##*/} --image IMAGE@sha256:DIGEST --version VERSION --commit SHA --ref REF --build-time RFC3339" >&2
}

image=
version=
commit=
ref=
build_time=
while (($#)); do
  case "$1" in
    --image) image=${2-}; shift 2 ;;
    --version) version=${2-}; shift 2 ;;
    --commit) commit=${2-}; shift 2 ;;
    --ref) ref=${2-}; shift 2 ;;
    --build-time) build_time=${2-}; shift 2 ;;
    *) usage; exit 2 ;;
  esac
done

if [[ ! "$image" =~ @sha256:[0-9a-f]{64}$ || -z "$version" || -z "$commit" || -z "$ref" || -z "$build_time" ]]; then
  usage
  exit 2
fi
for command in docker jq; do
  command -v "$command" >/dev/null || {
    printf 'Required command not found: %s\n' "$command" >&2
    exit 1
  }
done

# Pulling the canonical digest is intentional: a locally built tag is not a
# valid substitute for post-publication registry verification.
docker pull --platform linux/amd64 "$image"
[[ $(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image") == linux/amd64 ]]
[[ $(docker image inspect --format '{{.Config.User}}' "$image") == 0:0 ]]

assert_label() {
  local label=$1 expected=$2 actual
  actual=$(docker image inspect --format "{{index .Config.Labels \"$label\"}}" "$image")
  [[ "$actual" == "$expected" ]] || {
    printf 'Image label %s is %q, want %q\n' "$label" "$actual" "$expected" >&2
    exit 1
  }
}

assert_label org.opencontainers.image.version "$version"
assert_label org.opencontainers.image.revision "$commit"
assert_label org.opencontainers.image.ref.name "$ref"
assert_label org.opencontainers.image.created "$build_time"
assert_label org.opencontainers.image.title qtap
assert_label org.opencontainers.image.description 'An eBPF agent that captures pre-encrypted network traffic, providing rich context about egress connections and their originating processes.'
assert_label org.opencontainers.image.source https://github.com/qpoint-io/qtap
assert_label org.opencontainers.image.url https://github.com/qpoint-io/qtap
assert_label org.opencontainers.image.licenses AGPL-3.0-only

metadata=$(docker run --rm --platform linux/amd64 "$image" build-info)
jq -e \
  --arg version "$version" \
  --arg commit "$commit" \
  --arg ref "$ref" \
  --arg build_time "$build_time" '
    .version == $version and .commit == $commit and .ref == $ref and
    .build_time == $build_time and
    .source == "https://github.com/qpoint-io/qtap" and
    .license == "AGPL-3.0-only" and
    .os == "linux" and .architecture == "amd64"
  ' <<<"$metadata" >/dev/null
docker run --rm --platform linux/amd64 "$image" --version | grep -F -- "$version" >/dev/null

cid=$(docker run -d \
  --platform linux/amd64 \
  --privileged \
  --pid=host \
  --network=host \
  --ulimit=memlock=-1 \
  -v /sys:/sys:ro \
  "$image" \
  --httpd-listen=127.0.0.1:0 \
  --tls-probes=openssl)
trap 'docker rm -f "$cid" >/dev/null 2>&1 || true' EXIT
sleep 5
if [[ $(docker inspect --format '{{.State.Running}}' "$cid") != true ]]; then
  docker logs "$cid" >&2
  exit 1
fi
docker stop -t 15 "$cid" >/dev/null
[[ $(docker inspect --format '{{.State.ExitCode}}' "$cid") == 0 ]]
