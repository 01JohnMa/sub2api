#!/usr/bin/env bash
set -euo pipefail

config=/Users/lawrence/.codex/config.toml
: "${CPA_REMOTE_HOST:?set approved production SSH target}"
local_port=18083
tmpdir="$(mktemp -d /private/tmp/cpa-v104-item-id.XXXXXX)"
tunnel_pid=""

cleanup() {
  if [ -n "$tunnel_pid" ]; then
    kill "$tunnel_pid" 2>/dev/null || true
    wait "$tunnel_pid" 2>/dev/null || true
  fi
  rm -rf "$tmpdir"
}
trap cleanup EXIT
emit() { printf '%s=%s\n' "$1" "$2"; }
fail() { printf 'item_id_baseline_error=%s\n' "$1" >&2; exit 1; }

nc -z 127.0.0.1 "$local_port" 2>/dev/null && fail local_port_busy
ssh -o BatchMode=yes -o ConnectTimeout=12 -o ExitOnForwardFailure=yes -N -L "$local_port:127.0.0.1:8080" "$CPA_REMOTE_HOST" &
tunnel_pid=$!
for _ in $(seq 1 30); do
  nc -z 127.0.0.1 "$local_port" 2>/dev/null && break
  sleep 0.2
done
nc -z 127.0.0.1 "$local_port" 2>/dev/null || fail tunnel_not_ready

token="$(awk -F= '/^[[:space:]]*experimental_bearer_token[[:space:]]*=/ {v=$2; sub(/^[[:space:]]*/,"",v); sub(/[[:space:]]*$/, "", v); sub(/^"/,"",v); sub(/"$/, "", v); print v; exit}' "$config")"
[ -n "$token" ] || fail bearer_token_missing
cat > "$tmpdir/request.curl" <<EOF
silent
show-error
connect-timeout = 10
max-time = 180
header = "Authorization: Bearer $token"
header = "Content-Type: application/json"
EOF
chmod 0600 "$tmpdir/request.curl"
unset token
base="http://127.0.0.1:$local_port"

redact_message() {
  sed -E 's/msg_[A-Za-z0-9_-]+/<message_id>/g; s/call_[A-Za-z0-9_-]+/<call_id>/g; s/resp_[A-Za-z0-9_-]+/<response_id>/g' | cut -c1-240
}

jq -n '{model:"gpt-5.6-sol",input:"Reply with exactly ID-SEED.",stream:false,prompt_cache_key:"cpa-v104-item-id-seed"}' > "$tmpdir/seed-request.json"
seed_http="$(curl --config "$tmpdir/request.curl" -o "$tmpdir/seed.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/seed-request.json" "$base/v1/responses")"
message_item="$(jq -c '[.output[]? | select(.type=="message")] | first // {}' "$tmpdir/seed.json")"
message_id="$(printf '%s' "$message_item" | jq -r '.id // empty')"

compliant_http=skipped
overlong_http=skipped
printf '{}' > "$tmpdir/compliant.json"
printf '{}' > "$tmpdir/overlong.json"
if [ "$seed_http" = 200 ] && [ -n "$message_id" ]; then
  jq -n --argjson item "$message_item" '{model:"gpt-5.6-sol",input:[$item,{role:"user",content:"Reply with exactly ID-COMPLIANT."}],stream:false,prompt_cache_key:"cpa-v104-item-id-compliant"}' > "$tmpdir/compliant-request.json"
  compliant_http="$(curl --config "$tmpdir/request.curl" -o "$tmpdir/compliant.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/compliant-request.json" "$base/v1/responses")"

  overlong_id="msg_$(printf 'x%.0s' $(seq 1 80))"
  overlong_item="$(printf '%s' "$message_item" | jq -c --arg id "$overlong_id" '.id=$id')"
  jq -n --argjson item "$overlong_item" '{model:"gpt-5.6-sol",input:[$item,{role:"user",content:"Reply with exactly ID-OVERLONG."}],stream:false,prompt_cache_key:"cpa-v104-item-id-overlong"}' > "$tmpdir/overlong-request.json"
  overlong_http="$(curl --config "$tmpdir/request.curl" -o "$tmpdir/overlong.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/overlong-request.json" "$base/v1/responses")"
fi

emit seed_http "$seed_http"
emit generated_message_id_length "${#message_id}"
emit generated_message_id_sha256 "$(printf '%s' "$message_id" | shasum -a 256 | cut -d ' ' -f 1)"
emit compliant_replay_http "$compliant_http"
emit compliant_text_nonempty "$(jq '[.output[]?.content[]? | select(.type=="output_text") | .text | select(length>0)] | length > 0' "$tmpdir/compliant.json")"
emit compliant_error_type "$(jq -r '.error.type // .type // ""' "$tmpdir/compliant.json")"
emit compliant_error_code "$(jq -r '.error.code // ""' "$tmpdir/compliant.json")"
emit compliant_error_message_redacted "$(jq -r '.error.message // .message // ""' "$tmpdir/compliant.json" | redact_message)"
emit overlong_id_length "$(test -n "${overlong_id:-}" && printf '%s' "${#overlong_id}" || printf 0)"
emit overlong_replay_http "$overlong_http"
emit overlong_text_nonempty "$(jq '[.output[]?.content[]? | select(.type=="output_text") | .text | select(length>0)] | length > 0' "$tmpdir/overlong.json")"
emit overlong_error_type "$(jq -r '.error.type // .type // ""' "$tmpdir/overlong.json")"
emit overlong_error_code "$(jq -r '.error.code // ""' "$tmpdir/overlong.json")"
emit overlong_error_message_redacted "$(jq -r '.error.message // .message // ""' "$tmpdir/overlong.json" | redact_message)"
