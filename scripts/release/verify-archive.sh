#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf '%s\n' "Usage: ${0##*/} --archive PATH --checksums PATH --version VERSION --commit SHA --ref REF --build-time RFC3339" >&2
}

archive=
checksums=
expected_version=
expected_commit=
expected_ref=
expected_build_time=
while (($#)); do
  case "$1" in
    --archive) archive=${2-}; shift 2 ;;
    --checksums) checksums=${2-}; shift 2 ;;
    --version) expected_version=${2-}; shift 2 ;;
    --commit) expected_commit=${2-}; shift 2 ;;
    --ref) expected_ref=${2-}; shift 2 ;;
    --build-time) expected_build_time=${2-}; shift 2 ;;
    *) usage; exit 2 ;;
  esac
done

if [[ -z "$archive" || -z "$checksums" || -z "$expected_version" || -z "$expected_commit" || -z "$expected_ref" || -z "$expected_build_time" ]]; then
  usage
  exit 2
fi
[[ -f "$archive" && -f "$checksums" ]] || {
  printf 'Archive or checksum file does not exist\n' >&2
  exit 1
}

for command in jq sha256sum stat tar; do
  command -v "$command" >/dev/null || {
    printf 'Required command not found: %s\n' "$command" >&2
    exit 1
  }
done

archive=$(cd -- "$(dirname -- "$archive")" && pwd)/$(basename -- "$archive")
checksums=$(cd -- "$(dirname -- "$checksums")" && pwd)/$(basename -- "$checksums")
archive_dir=${archive%/*}
archive_name=${archive##*/}

archive_in_sums=false
while read -r digest marker_and_name; do
  name=${marker_and_name#\*}
  name=${name# }
  [[ "$digest" =~ ^[0-9a-f]{64}$ && -n "$name" ]] || {
    printf 'Malformed SHA256SUMS entry\n' >&2
    exit 1
  }
  [[ "$name" != */* && "$name" != .* && "$name" != -* ]] || {
    printf 'Unsafe SHA256SUMS path: %s\n' "$name" >&2
    exit 1
  }
  [[ -f "$archive_dir/$name" ]] || {
    printf 'Checksummed file is missing: %s\n' "$name" >&2
    exit 1
  }
  [[ "$name" == "$archive_name" ]] && archive_in_sums=true
done <"$checksums"
$archive_in_sums || {
  printf 'Archive is not listed in SHA256SUMS\n' >&2
  exit 1
}
(
  cd -- "$archive_dir"
  sha256sum -c -- "${checksums##*/}"
)

mapfile -t entries < <(tar -tzf "$archive")
if ((${#entries[@]} != 3)); then
  printf 'Archive must contain exactly three files\n' >&2
  exit 1
fi
for expected in LICENSE qtap release-metadata.json; do
  found=false
  for entry in "${entries[@]}"; do
    [[ "$entry" == "$expected" ]] && found=true
    [[ "$entry" != /* && "$entry" != ../* && "$entry" != */../* && "$entry" != */.. ]] || {
      printf 'Unsafe archive path: %s\n' "$entry" >&2
      exit 1
    }
  done
  $found || {
    printf 'Archive entry is missing: %s\n' "$expected" >&2
    exit 1
  }
done

declare -A stored_modes=(
  [LICENSE]=-rw-r--r--
  [qtap]=-rwxr-xr-x
  [release-metadata.json]=-rw-r--r--
)
while read -r mode _ _ _ _ name; do
	[[ "$mode" == -* ]] || {
		printf 'Archive contains a non-regular entry\n' >&2
		exit 1
	}
	[[ ${stored_modes[$name]-} == "$mode" ]] || {
		printf 'Archive mode for %s is %s, want %s\n' "$name" "$mode" "${stored_modes[$name]-missing}" >&2
		exit 1
	}
done < <(tar -tvzf "$archive")

extract_dir=$(mktemp -d)
trap 'rm -rf -- "$extract_dir"' EXIT
tar --extract --gzip --file "$archive" --directory "$extract_dir" --no-same-owner --same-permissions

[[ $(stat -c '%a' "$extract_dir/qtap") == 755 ]] || {
  printf 'qtap archive mode is not 0755\n' >&2
  exit 1
}
[[ $(stat -c '%a' "$extract_dir/LICENSE") == 644 && $(stat -c '%a' "$extract_dir/release-metadata.json") == 644 ]] || {
  printf 'Archive metadata modes are not 0644\n' >&2
  exit 1
}

metadata="$extract_dir/release-metadata.json"
jq -e \
  --arg version "$expected_version" \
  --arg commit "$expected_commit" \
  --arg ref "$expected_ref" \
  --arg build_time "$expected_build_time" '
    .version == $version and
    .commit == $commit and
    .ref == $ref and
    .build_time == $build_time and
    .source == "https://github.com/qpoint-io/qtap" and
    .license == "AGPL-3.0-only" and
    .os == "linux" and
    .architecture == "amd64"
  ' "$metadata" >/dev/null

binary_metadata=$("$extract_dir/qtap" build-info)
diff -u <(jq -S . "$metadata") <(jq -S . <<<"$binary_metadata")
version_output=$("$extract_dir/qtap" --version)
grep -F -- "$expected_version" <<<"$version_output" >/dev/null

printf 'Verified %s\n' "$archive_name"
