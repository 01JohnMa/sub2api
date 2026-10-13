#!/usr/bin/env bash
set -Eeuo pipefail

: "${CPA_REMOTE_HOST:?set the approved production SSH target}"

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
switch_script=$script_dir/switch-process.sh
rollback_script=$script_dir/rollback-process.sh
validator=$script_dir/validate-immediate.sh
baseline_collector=$script_dir/collect-pre-switch-baseline.sh
conditional_usage_script=$script_dir/check-conditional-usage-remote.sh
claim_owner_script=$script_dir/claim-owner-remote.sh
release_owner_script=$script_dir/release-owner-remote.sh
manifest=$script_dir/manifest.sha256
manifest_sha=be9e24f75f965a377ed8791c4f2f0292182120f3fe9b76fb65527e83ad859e6f
controller_normalized_sha=f2139bafaf148a2b129b61d03a07d630a7e3c15324ca46180a25d0fdcd0bb38f
baseline_file=""
artifact_snapshot=""
run_id="cpa-v104-$(date -u +%Y%m%dT%H%M%SZ)-$$-$(uuidgen | tr '[:upper:]' '[:lower:]')"
claim_attempted=0
owner_confirmed=0
release_attempted=0
committed=0

cleanup() {
  rm -f -- "$baseline_file"
  if test -n "$artifact_snapshot"; then
    chmod -R u+w "$artifact_snapshot" 2>/dev/null || true
    rm -rf -- "$artifact_snapshot"
  fi
}

rollback_remote() {
  local rc
  local owner_policy=require-match
  if ((owner_confirmed == 1 || release_attempted == 1)); then
    owner_policy=allow-absent
  fi
  if run_with_timeout 150 \
    ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" bash -s -- "$run_id" "$owner_policy" \
    < "$rollback_script"; then
    return 0
  else
    rc=$?
  fi
  if test "$rc" = 76; then
    printf '%s\n' 'rollback_skipped=owned_by_another_controller'
    return 0
  fi
  return "$rc"
}

claim_owner_remote() {
  run_with_timeout 30 \
    ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" bash -s -- "$run_id" \
    < "$claim_owner_script"
}

release_owner_remote() {
  run_with_timeout 90 \
    ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" bash -s -- "$run_id" \
    < "$release_owner_script"
}

on_controller_exit() {
  local rc=$?
  trap - EXIT INT TERM HUP
  cancel_active_timeout_command
  if ((claim_attempted == 1 && committed == 0)); then
    if ! rollback_remote; then
      printf '%s\n' 'FATAL: controller-triggered rollback did not verify; use the fixed rollback script from a second shell' >&2
      cleanup
      exit 70
    fi
  fi
  cleanup
  exit "$rc"
}

actual_controller_normalized_sha=$(sed 's/^controller_normalized_sha=.*/controller_normalized_sha=SELF/' "$0" | sha256sum | awk '{print $1}')
test "$actual_controller_normalized_sha" = "$controller_normalized_sha"
test "$(sha256sum "$manifest" | awk '{print $1}')" = "$manifest_sha"
(cd "$script_dir" && sha256sum -c manifest.sha256)

if test "${CPA_VERIFY_ONLY:-0}" = 1; then
  printf '%s\n' 'controller and artifact manifest verification passed'
  exit 0
fi

source "$script_dir/lib-local.sh"
artifact_snapshot=$(freeze_artifact_snapshot "$script_dir" "$manifest_sha")
trap on_controller_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP
test "$(sed 's/^controller_normalized_sha=.*/controller_normalized_sha=SELF/' "$artifact_snapshot/controller.sh" | sha256sum | awk '{print $1}')" = "$controller_normalized_sha"

script_dir=$artifact_snapshot
switch_script=$script_dir/switch-process.sh
rollback_script=$script_dir/rollback-process.sh
validator=$script_dir/validate-immediate.sh
baseline_collector=$script_dir/collect-pre-switch-baseline.sh
conditional_usage_script=$script_dir/check-conditional-usage-remote.sh
claim_owner_script=$script_dir/claim-owner-remote.sh
release_owner_script=$script_dir/release-owner-remote.sh
manifest=$script_dir/manifest.sha256
source "$script_dir/lib-local.sh"
baseline_file=$(mktemp /private/tmp/cpa-v104-pre-switch-baseline.XXXXXX)

if ! conditional_usage_output=$(run_with_timeout 45 \
  ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" bash -s -- 24 \
  < "$conditional_usage_script"); then
  printf '%s\n' 'conditional_usage_check=failed' >&2
  exit 78
fi
printf '%s\n' "$conditional_usage_output"
printf '%s\n' "$conditional_usage_output" | grep -Fq 'conditional_usage=absent'

export CPA_BASELINE_FILE=$baseline_file
"$baseline_collector"

printf 'run_id=%s\n' "$run_id"
claim_attempted=1
claim_owner_remote
owner_confirmed=1
run_with_timeout 120 \
  ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" bash -s -- "$run_id" \
  < "$switch_script"

run_with_timeout 5400 "$validator"
release_attempted=1
release_owner_remote
committed=1
printf '%s\n' 'immediate and short-observation gates passed; automatic rollback controller disarmed'
