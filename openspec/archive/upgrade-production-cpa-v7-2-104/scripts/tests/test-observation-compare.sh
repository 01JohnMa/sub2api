#!/usr/bin/env bash
set -Eeuo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
compare=$script_dir/../compare-observation.sh
tmpdir=$(mktemp -d /private/tmp/cpa-v104-observation-test.XXXXXX)
trap 'rm -rf -- "$tmpdir"' EXIT

baseline=$tmpdir/baseline
cat > "$baseline" <<'EOF'
nginx_15={"endpoints":[{"endpoint":"/v1/responses","status":200,"count":99,"total":100},{"endpoint":"/v1/responses","status":503,"count":1,"total":100}]}
usage_15={"p95_duration_ms":100,"p95_ttft_ms":50}
errors_15={"signatures":[{"endpoint":"/v1/responses","status":503,"signature":"known","count":1}]}
nginx_30={"endpoints":[{"endpoint":"/v1/responses","status":200,"count":99,"total":100},{"endpoint":"/v1/responses","status":503,"count":1,"total":100}]}
usage_30={"p95_duration_ms":100,"p95_ttft_ms":50}
errors_30={"signatures":[{"endpoint":"/v1/responses","status":503,"signature":"known","count":1}]}
EOF

write_post() {
  local file=$1 status_count=$2 signature=$3 signature_count=$4
  printf '{"endpoints":[{"endpoint":"/v1/responses","status":200,"count":%s,"total":100},{"endpoint":"/v1/responses","status":503,"count":%s,"total":100}]}\n' "$((100-status_count))" "$status_count" > "$file"
  printf '%s\n' '{"p95_duration_ms":120,"p95_ttft_ms":60}' >> "$file"
  printf '{"signatures":[{"endpoint":"/v1/responses","status":503,"signature":"%s","count":%s}]}\n' "$signature" "$signature_count" >> "$file"
}

write_post_without_signature() {
  local file=$1 status_count=$2
  printf '{"endpoints":[{"endpoint":"/v1/responses","status":200,"count":%s,"total":100},{"endpoint":"/v1/responses","status":503,"count":%s,"total":100}]}\n' "$((100-status_count))" "$status_count" > "$file"
  printf '%s\n' '{"p95_duration_ms":120,"p95_ttft_ms":60}' >> "$file"
  printf '%s\n' '{"signatures":[]}' >> "$file"
}

expect_pass() {
  local label=$1 status_count=$2 signature=$3 signature_count=$4 post
  post=$tmpdir/$label.post
  write_post "$post" "$status_count" "$signature" "$signature_count"
  "$compare" "$baseline" "$post" 15 >/dev/null
  printf 'observation_pass_%s=passed\n' "$label"
}

expect_fail() {
  local label=$1 status_count=$2 signature=$3 signature_count=$4 post
  post=$tmpdir/$label.post
  write_post "$post" "$status_count" "$signature" "$signature_count"
  if "$compare" "$baseline" "$post" 15 >/dev/null 2>&1; then
    exit 1
  fi
  printf 'observation_fail_%s=passed\n' "$label"
}

expect_pass known_signature_at_threshold 3 known 3
expect_pass new_signature_at_one_percent 1 new 1
expect_fail new_signature_over_one_percent 2 new 2
expect_fail known_signature_over_tolerance 4 known 4

four_x_post=$tmpdir/four_x.post
printf '%s\n' '{"endpoints":[{"endpoint":"/v1/responses","status":200,"count":90,"total":100},{"endpoint":"/v1/responses","status":401,"count":10,"total":100}]}' '{"p95_duration_ms":120,"p95_ttft_ms":60}' '{"signatures":[{"endpoint":"/v1/responses","status":401,"signature":"auth","count":10}]}' > "$four_x_post"
"$compare" "$baseline" "$four_x_post" 15 >/dev/null
printf 'observation_pass_4xx_outside_5xx_gate=passed\n'

unclassified_one=$tmpdir/unclassified-one.post
write_post_without_signature "$unclassified_one" 1
"$compare" "$baseline" "$unclassified_one" 15 >/dev/null
printf 'observation_pass_unclassified_at_one_percent=passed\n'

unclassified_two=$tmpdir/unclassified-two.post
write_post_without_signature "$unclassified_two" 2
if "$compare" "$baseline" "$unclassified_two" 15 >/dev/null 2>&1; then
  exit 1
fi
printf 'observation_fail_unclassified_over_one_percent=passed\n'
