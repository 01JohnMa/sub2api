#!/usr/bin/env bash
set -Eeuo pipefail

config=${CPA_CODEX_CONFIG:-/Users/lawrence/.codex/config.toml}
tmpdir=$(mktemp -d /private/tmp/cpa-v104-models.XXXXXX)
cleanup() { rm -rf -- "$tmpdir"; }
trap cleanup EXIT

fail() {
  printf 'model_validation_error=%s\n' "$1" >&2
  exit 1
}

token=$(awk -F= '/^[[:space:]]*experimental_bearer_token[[:space:]]*=/ {v=$2; sub(/^[[:space:]]*/,"",v); sub(/[[:space:]]*$/, "", v); sub(/^"/,"",v); sub(/"$/, "", v); print v; exit}' "$config")
test -n "$token" || fail bearer_token_missing
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

models='codex-auto-review gpt-5.4 gpt-5.4-mini gpt-5.5 gpt-5.6-luna gpt-5.6-sol gpt-5.6-terra'
for model in $models; do
  jq -n --arg model "$model" '{model:$model,input:"Reply with exactly MODEL-OK.",stream:false}' > "$tmpdir/request.json"
  status=$(curl --config "$tmpdir/request.curl" -o "$tmpdir/response.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/request.json" https://coococode.com/v1/responses)
  test "$status" = 200 || fail "${model}_http_$status"
  test "$(jq '[.output[]?.content[]? | select(.type=="output_text") | .text | select(length>0)] | length > 0' "$tmpdir/response.json")" = true || fail "${model}_empty_output"
  test "$(jq -r '.usage.input_tokens // 0' "$tmpdir/response.json")" -gt 0 || fail "${model}_input_tokens"
  test "$(jq -r '.usage.output_tokens // 0' "$tmpdir/response.json")" -gt 0 || fail "${model}_output_tokens"
  printf 'model_case=%s,200\n' "$model"
done

printf 'models_used_check=passed\n'
