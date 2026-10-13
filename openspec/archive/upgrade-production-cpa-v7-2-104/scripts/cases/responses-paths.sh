#!/usr/bin/env bash
set -euo pipefail
umask 077

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source "$script_dir/../validator-case-runner.sh"

config=/Users/lawrence/.codex/config.toml
: "${CPA_REMOTE_HOST:?set approved production SSH target}"
local_port=18080
tmpdir="$(mktemp -d /private/tmp/cpa-v104-public.XXXXXX)"
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
fail() { printf 'public_baseline_error=%s\n' "$1" >&2; exit 1; }

[ -r "$config" ] || fail config_missing
token="$(awk -F= '/^[[:space:]]*experimental_bearer_token[[:space:]]*=/ {v=$2; sub(/^[[:space:]]*/,"",v); sub(/[[:space:]]*$/, "", v); sub(/^"/,"",v); sub(/"$/, "", v); print v; exit}' "$config")"
[ -n "$token" ] || fail bearer_token_missing

if nc -z 127.0.0.1 "$local_port" 2>/dev/null; then
  fail local_tunnel_port_busy
fi
ssh -o BatchMode=yes -o ConnectTimeout=12 -o ExitOnForwardFailure=yes \
  -N -L "$local_port:127.0.0.1:8080" "$CPA_REMOTE_HOST" &
tunnel_pid=$!
for _ in $(seq 1 30); do
  nc -z 127.0.0.1 "$local_port" 2>/dev/null && break
  sleep 0.2
done
nc -z 127.0.0.1 "$local_port" 2>/dev/null || fail tunnel_not_ready

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

jq -n '{model:"gpt-5.6-sol",input:"Reply with exactly PUBLIC-BASELINE.",stream:false}' > "$tmpdir/request.json"
local_status="$(curl --config "$tmpdir/request.curl" -D "$tmpdir/local.headers" -o "$tmpdir/local.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/request.json" "http://127.0.0.1:18080/v1/responses")"
public_status="$(curl --config "$tmpdir/request.curl" -D "$tmpdir/public.headers" -o "$tmpdir/public.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/request.json" "https://coococode.com/v1/responses")"

for path in local public; do
  if [ "$path" = local ]; then status="$local_status"; else status="$public_status"; fi
  emit "${path}_http" "$status"
  emit "${path}_model" "$(jq -r '.model // ""' "$tmpdir/$path.json")"
  emit "${path}_text_nonempty" "$(jq '[.output[]?.content[]? | select(.type=="output_text") | .text | select(length>0)] | length > 0' "$tmpdir/$path.json")"
  emit "${path}_input_tokens" "$(jq -r '.usage.input_tokens // 0' "$tmpdir/$path.json")"
  emit "${path}_cached_tokens" "$(jq -r '.usage.input_tokens_details.cached_tokens // 0' "$tmpdir/$path.json")"
  emit "${path}_output_tokens" "$(jq -r '.usage.output_tokens // 0' "$tmpdir/$path.json")"
  emit "${path}_reasoning_tokens" "$(jq -r '.usage.output_tokens_details.reasoning_tokens // 0' "$tmpdir/$path.json")"
  emit "${path}_error_type" "$(jq -r '.error.type // .type // ""' "$tmpdir/$path.json")"
  emit "${path}_error_code" "$(jq -r '.error.code // ""' "$tmpdir/$path.json")"
  emit "${path}_cpa_trace_header_count" "$(http_final_header_count "$tmpdir/$path.headers" X-CPA-TRACE-ID)"
done
