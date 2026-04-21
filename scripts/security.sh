#!/usr/bin/env bash
# Runs govulncheck and filters findings against .govulncheck-allow.
#
# A finding is suppressed if its OSV id or any alias (GHSA-*, CVE-*) is listed
# in the allowlist. Suppressed findings are still printed, but do not affect
# the exit code. Any non-allowlisted finding causes a non-zero exit.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALLOWLIST="${ALLOWLIST:-$ROOT_DIR/.govulncheck-allow}"
PKGS="${1:-./...}"

if ! command -v jq >/dev/null 2>&1; then
  echo "error: jq is required" >&2
  exit 2
fi

allow_json='[]'
if [[ -f "$ALLOWLIST" ]]; then
  allow_json="$(sed 's/#.*$//' "$ALLOWLIST" | tr -d '[:blank:]' | grep -v '^$' | jq -R . | jq -sc .)"
fi

raw="$(mktemp)"
trap 'rm -f "$raw"' EXIT

# govulncheck exits non-zero on findings; capture and re-evaluate ourselves.
set +e
go tool govulncheck -format=json "$PKGS" >"$raw"
set -e

jq -s --argjson allow "$allow_json" '
  # Index OSV records by id
  (map(select(.osv)) | map({key: .osv.id, value: .osv}) | from_entries) as $osvs
  | (map(select(.finding)) | map(.finding)) as $findings
  | ($allow | map(ascii_upcase) | unique) as $allow
  | ([$findings[].osv] | unique) as $ids
  | {
      allowed: [ $ids[] | . as $id | $osvs[$id] // {id:$id, aliases:[], summary:""}
                 | select([.id, (.aliases // [])[]] | map(ascii_upcase) | any(. as $x | $allow | index($x))) ],
      active:  [ $ids[] | . as $id | $osvs[$id] // {id:$id, aliases:[], summary:""}
                 | select([.id, (.aliases // [])[]] | map(ascii_upcase) | any(. as $x | $allow | index($x)) | not) ],
      findings_by_osv: ($findings | group_by(.osv) | map({key: .[0].osv, value: .}) | from_entries),
    }
' "$raw" >"$raw.parsed"

active_count="$(jq '.active | length' "$raw.parsed")"
allowed_count="$(jq '.allowed | length' "$raw.parsed")"

if [[ "$allowed_count" -gt 0 ]]; then
  echo "Suppressed vulnerabilities ($allowed_count) — see $ALLOWLIST:"
  jq -r '.allowed[] | "  - \(.id)\( if (.aliases // []) | length > 0 then " (" + (.aliases | join(", ")) + ")" else "" end ): \(.summary)"' "$raw.parsed"
  echo
fi

if [[ "$active_count" -eq 0 ]]; then
  echo "No active vulnerabilities."
  exit 0
fi

echo "Active vulnerabilities ($active_count):"
jq -r '
  .active[] as $osv
  | .findings_by_osv[$osv.id] as $fs
  | ($fs | map(select(.trace[0].function != null and .trace[0].function != "init"))) as $calls
  | "  - \($osv.id)\( if ($osv.aliases // []) | length > 0 then " (" + ($osv.aliases | join(", ")) + ")" else "" end ): \($osv.summary)",
    ( if ($calls | length) > 0
      then "      reachable symbols: " + ($calls | map((.trace[0].package // "?") + "." + .trace[0].function) | unique | join(", "))
      else empty end ),
    ( $fs | map(.fixed_version) | map(select(. != null)) | unique
      | if length > 0 then "      fixed in: " + join(", ") else empty end )
' "$raw.parsed"

exit 1
