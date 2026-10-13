#!/usr/bin/env bash
set -Eeuo pipefail

run_id=${1:?run id required}
if [[ ! "$run_id" =~ ^cpa-v104-[A-Za-z0-9.-]+$ ]]; then
  printf 'owner_claim_error=invalid_run_id\n' >&2
  exit 2
fi

lock_dir=/run/cpa-v104-switch
owner_file=$lock_dir/owner
lock_file=$lock_dir/lock
owner_tmp=$lock_dir/.owner.$$
formal=/opt/cpa/bin/cli-proxy-api
rollback_sha=e2ea31a127240f213d046567721839d725eeb4c136225eef139c6a46aa464b17
rollback_version='CLIProxyAPI Version: 7.2.82, Commit: f583414f, BuiltAt: 2026-07-17T08:17:41Z'
umask 077
if ! test -e "$lock_dir"; then
  install -d -o root -g root -m 0700 "$lock_dir"
fi
test ! -L "$lock_dir"
test -d "$lock_dir"
test "$(stat -c '%U:%G %a' "$lock_dir")" = 'root:root 700'
if ! test -e "$lock_file"; then
  install -o root -g root -m 0600 /dev/null "$lock_file"
fi
test ! -L "$lock_file"
test -f "$lock_file"
test "$(stat -c '%U:%G %a' "$lock_file")" = 'root:root 600'
exec 9<>"$lock_file"
flock -w 5 9 || {
  printf 'owner_claim_error=lock_busy\n' >&2
  exit 75
}

if test -f "$owner_file"; then
  test ! -L "$owner_file"
  test "$(stat -c '%U:%G %a' "$owner_file")" = 'root:root 600'
  current_owner=$(sed -n '1p' "$owner_file")
  if test "$current_owner" = "$run_id"; then
    printf 'owner_claim=already_owned\n'
    exit 0
  fi
  printf 'owner_claim_error=owned_by_another_controller\n' >&2
  exit 75
fi

source_state_mismatch() {
  printf 'owner_claim_error=source_state_not_v7.2.82\n' >&2
  exit 77
}

active_state=$(systemctl show -p ActiveState --value cpa.service) || source_state_mismatch
test "$active_state" = active || source_state_mismatch
main_pid=$(systemctl show -p MainPID --value cpa.service) || source_state_mismatch
case "$main_pid" in
  ''|*[!0-9]*) source_state_mismatch ;;
esac
test "$main_pid" -gt 0 || source_state_mismatch
test "$(readlink -f "/proc/$main_pid/exe")" = "$formal" || source_state_mismatch
test "$(sha256sum "$formal" | awk '{print $1}')" = "$rollback_sha" || source_state_mismatch
set +e
version_output=$(timeout --foreground --signal=TERM --kill-after=2 10s "$formal" -version 2>&1)
version_rc=$?
set -e
test "$version_rc" = 2 || source_state_mismatch
test "$(printf '%s\n' "$version_output" | awk '/^CLIProxyAPI Version: / {print; exit}')" = "$rollback_version" || source_state_mismatch
ss -ltnH | awk '$4 ~ /:8317$/ {found=1} END {exit !found}' || source_state_mismatch

printf '%s\n' "$run_id" > "$owner_tmp"
chmod 0600 "$owner_tmp"
mv -f "$owner_tmp" "$owner_file"
printf 'owner_claim=created\n'
