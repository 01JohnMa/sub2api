#!/usr/bin/env bash
set -Eeuo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
validator=$script_dir/../validate-log-remote.sh
lib=$script_dir/../lib-local.sh
log_case=$script_dir/../cases/log-rate.sh
immediate_validator=$script_dir/../validate-immediate.sh
tmpdir=$(mktemp -d /private/tmp/cpa-v104-log-validator-test.XXXXXX)
trap 'rm -rf -- "$tmpdir"' EXIT

helpers=$tmpdir/helpers.sh
sed -n '/^# BEGIN LOG VALIDATION HELPERS$/,/^# END LOG VALIDATION HELPERS$/p' "$validator" \
  | sed '/^# BEGIN LOG VALIDATION HELPERS$/d; /^# END LOG VALIDATION HELPERS$/d' > "$helpers"
source "$helpers"
source "$lib"

pattern=$(build_sensitive_pattern "$CPA_LOG_BODY_CANARY" "$CPA_LOG_SESSION_CANARY")
test "$(printf '%s\n' benign | count_sensitive_matches "$pattern")" = 0
test "$(printf '%s\n' 'Authorization: redacted' | count_sensitive_matches "$pattern")" = 1
for structured in \
  '{"cookie":"session-secret"}' \
  '{"api_key":"sk-secret"}' \
  '{"x-api-key":"sk-secret"}' \
  '{"xApiKey":"sk-secret"}' \
  '{"authorization":"token-secret"}'; do
  test "$(printf '%s\n' "$structured" | count_sensitive_matches "$pattern")" = 1
done
for privacy_leak in \
  "request=$CPA_LOG_BODY_CANARY" \
  "prompt_cache_key=$CPA_LOG_SESSION_CANARY" \
  'response=resp_1234567890abcdef' \
  'tool=call_1234567890abcdef' \
  'message=msg_1234567890abcdef' \
  'session=sess_1234567890abcdef' \
  'X-CPA-TRACE-ID: redacted' \
  'trace=20260802112233-0123456789abcdef-deadbeef'; do
  test "$(printf '%s\n' "$privacy_leak" | count_sensitive_matches "$pattern")" = 1
done
test "$(printf '%s\n' '{"authorization_status":"ok"}' | count_sensitive_matches "$pattern")" = 0
test "$(printf '%s\n' 'call_id_sha256=redacted' | count_sensitive_matches "$pattern")" = 0
if build_sensitive_pattern 'unsafe.*' "$CPA_LOG_SESSION_CANARY" >/dev/null 2>&1; then exit 1; fi

set +e
partial_count=$( (printf '%s\n' benign; exit 42) | count_sensitive_matches "$pattern")
partial_rc=$?
set -e
test "$partial_count" = 0
test "$partial_rc" = 42

fakebin=$tmpdir/fakebin
mkdir "$fakebin"
printf '%s\n' '#!/usr/bin/env bash' 'exit 43' > "$fakebin/find"
chmod 0755 "$fakebin/find"
if PATH="$fakebin:$PATH" write_log_file_list "$tmpdir" "$tmpdir/list"; then
  exit 1
fi

if grep -Fq '|| true' "$validator"; then exit 1; fi
if grep -Fq '< <(find' "$validator"; then exit 1; fi
grep -Fq -- '--arg body_canary "$CPA_LOG_BODY_CANARY"' "$log_case"
grep -Fq -- '--arg session_canary "$CPA_LOG_SESSION_CANARY"' "$log_case"
grep -Fq 'prompt_cache_key:$session_canary' "$log_case"
grep -Fq '$CPA_LOG_BODY_CANARY $CPA_LOG_SESSION_CANARY' "$immediate_validator"

printf 'log_validator_fail_closed=passed\n'
