#!/usr/bin/env bash
set -euo pipefail

config=/Users/lawrence/.codex/config.toml
: "${CPA_REMOTE_HOST:?set approved production SSH target}"
local_port=18084
tmpdir="$(mktemp -d /private/tmp/cpa-v104-chat.XXXXXX)"
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

nc -z 127.0.0.1 "$local_port" 2>/dev/null && exit 2
ssh -o BatchMode=yes -o ConnectTimeout=12 -o ExitOnForwardFailure=yes -N -L "$local_port:127.0.0.1:8080" "$CPA_REMOTE_HOST" &
tunnel_pid=$!
for _ in $(seq 1 30); do
  nc -z 127.0.0.1 "$local_port" 2>/dev/null && break
  sleep 0.2
done
nc -z 127.0.0.1 "$local_port" 2>/dev/null

token="$(awk -F= '/^[[:space:]]*experimental_bearer_token[[:space:]]*=/ {v=$2; sub(/^[[:space:]]*/,"",v); sub(/[[:space:]]*$/, "", v); sub(/^"/,"",v); sub(/"$/, "", v); print v; exit}' "$config")"
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
jq -n '{model:"gpt-5.6-sol",messages:[{role:"user",content:"Reply with exactly CHAT-PREFLIGHT."}],stream:false,max_completion_tokens:32}' > "$tmpdir/request.json"

local_status="$(curl --config "$tmpdir/request.curl" -o "$tmpdir/local.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/request.json" "http://127.0.0.1:$local_port/v1/chat/completions")"
public_status="$(curl --config "$tmpdir/request.curl" -o "$tmpdir/public.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/request.json" "https://coococode.com/v1/chat/completions")"

for path in local public; do
  if [ "$path" = local ]; then status="$local_status"; else status="$public_status"; fi
  emit "${path}_http" "$status"
  emit "${path}_model" "$(jq -r '.model // ""' "$tmpdir/$path.json" 2>/dev/null || true)"
  emit "${path}_text_nonempty" "$(jq '[.choices[]?.message.content // "" | select(length>0)] | length > 0' "$tmpdir/$path.json" 2>/dev/null || echo false)"
  emit "${path}_input_tokens" "$(jq -r '.usage.prompt_tokens // 0' "$tmpdir/$path.json" 2>/dev/null || echo 0)"
  emit "${path}_cached_tokens" "$(jq -r '.usage.prompt_tokens_details.cached_tokens // 0' "$tmpdir/$path.json" 2>/dev/null || echo 0)"
  emit "${path}_output_tokens" "$(jq -r '.usage.completion_tokens // 0' "$tmpdir/$path.json" 2>/dev/null || echo 0)"
  emit "${path}_error_type" "$(jq -r '.error.type // .type // ""' "$tmpdir/$path.json" 2>/dev/null || true)"
  emit "${path}_error_code" "$(jq -r '.error.code // ""' "$tmpdir/$path.json" 2>/dev/null || true)"
done
