set -Eeuo pipefail
trap '' INT TERM HUP

run_id=${1:?run id required}
if [[ ! "$run_id" =~ ^cpa-v104-[A-Za-z0-9.-]+$ ]]; then
  printf 'rollback_error=invalid_run_id\n' >&2
  exit 2
fi
owner_policy=${2:-allow-absent}
case "$owner_policy" in
  require-match|allow-absent) ;;
  *)
    printf 'rollback_error=invalid_owner_policy\n' >&2
    exit 2
    ;;
esac

formal=/opt/cpa/bin/cli-proxy-api
rollback=/opt/cpa/backups/cpa-v7.2.82-20260729T111659Z/cli-proxy-api
rollback_sha=e2ea31a127240f213d046567721839d725eeb4c136225eef139c6a46aa464b17
rollback_version='CLIProxyAPI Version: 7.2.82, Commit: f583414f, BuiltAt: 2026-07-17T08:17:41Z'
lock_dir=/run/cpa-v104-switch
lock_file=$lock_dir/lock
owner_file=$lock_dir/owner
rollback_tmp=/opt/cpa/bin/.cli-proxy-api.v7.2.82.$$
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
flock -w 65 9 || {
  printf '%s\n' 'rollback_error=another_switch_or_rollback_did_not_release_lock' >&2
  exit 75
}
owner_matches=0
if test -f "$owner_file"; then
  test ! -L "$owner_file"
  test "$(stat -c '%U:%G %a' "$owner_file")" = 'root:root 600'
  current_owner=$(sed -n '1p' "$owner_file")
  if test "$current_owner" != "$run_id"; then
    printf '%s\n' 'rollback_skipped=owned_by_another_controller' >&2
    exit 76
  fi
  owner_matches=1
elif test "$owner_policy" = require-match; then
  printf '%s\n' 'rollback_skipped=owner_absent_before_confirmed_claim'
  exit 0
fi
deadline=$(($(date +%s) + 60))

run_until() {
  local remaining=$(( $1 - $(date +%s) ))
  shift
  ((remaining > 0)) || return 124
  timeout --foreground --signal=TERM --kill-after=2 "${remaining}s" "$@"
}

sha256_until() {
  local deadline_arg=$1
  local path=$2
  run_until "$deadline_arg" sha256sum "$path" | awk '{print $1}'
}

version_line_until() {
  local deadline_arg=$1
  local path=$2
  local output rc
  set +e
  output=$(run_until "$deadline_arg" "$path" -version 2>&1)
  rc=$?
  set -e
  test "$rc" = 2
  printf '%s\n' "$output" | awk '/^CLIProxyAPI Version: / {print; exit}'
}

listener_present_until() {
  local deadline_arg=$1
  local remaining=$((deadline_arg - $(date +%s)))
  ((remaining > 0)) || return 124
  timeout --foreground --signal=TERM --kill-after=2 "${remaining}s" ss -ltnH \
    | awk '$4 ~ /:8317$/ {found=1} END {exit !found}'
}

await_active_until() {
  local deadline_arg=$1
  local state
  while (( $(date +%s) < deadline_arg )); do
    state=$(run_until "$deadline_arg" systemctl show -p ActiveState --value cpa.service) || return $?
    if test "$state" = active && listener_present_until "$deadline_arg"; then
      return 0
    fi
    sleep 1
  done
  return 124
}

await_stopped_until() {
  local deadline_arg=$1
  local state pid
  while (( $(date +%s) < deadline_arg )); do
    state=$(run_until "$deadline_arg" systemctl show -p ActiveState --value cpa.service) || return $?
    pid=$(run_until "$deadline_arg" systemctl show -p MainPID --value cpa.service) || return $?
    if test "$state" != active && test "$pid" = 0 && ! listener_present_until "$deadline_arg"; then
      return 0
    fi
    sleep 1
  done
  return 124
}

stop_unit() {
  local kill_deadline=$(($(date +%s) + 5))
  if ((kill_deadline > deadline)); then
    kill_deadline=$deadline
  fi
  run_until "$deadline" systemctl stop --no-block cpa.service
  if await_stopped_until "$kill_deadline"; then
    return 0
  fi
  run_until "$deadline" systemctl kill --kill-who=all --signal=KILL cpa.service
  await_stopped_until "$deadline"
}

start_unit() {
  run_until "$deadline" systemctl start --no-block cpa.service
  await_active_until "$deadline"
}

current_pid=$(run_until "$deadline" systemctl show -p MainPID --value cpa.service)
if test "$(run_until "$deadline" systemctl show -p ActiveState --value cpa.service)" = active \
  && test "$current_pid" -gt 0 \
  && test "$(run_until "$deadline" readlink -f "/proc/$current_pid/exe")" = "$formal" \
  && listener_present_until "$deadline" \
  && test "$(sha256_until "$deadline" "$formal")" = "$rollback_sha" \
  && test "$(version_line_until "$deadline" "$formal")" = "$rollback_version"; then
  printf '%s\n' 'rollback gate already satisfied: active v7.2.82'
  if test "$owner_matches" = 1; then rm -f -- "$owner_file"; fi
  exit 0
fi

stop_unit
run_until "$deadline" install -o root -g root -m 0755 "$rollback" "$rollback_tmp"
test "$(sha256_until "$deadline" "$rollback_tmp")" = "$rollback_sha"
run_until "$deadline" mv -fT "$rollback_tmp" "$formal"
start_unit
restored_pid=$(run_until "$deadline" systemctl show -p MainPID --value cpa.service)
test "$restored_pid" -gt 0
test "$(run_until "$deadline" readlink -f "/proc/$restored_pid/exe")" = "$formal"
test "$(sha256_until "$deadline" "$formal")" = "$rollback_sha"
test "$(version_line_until "$deadline" "$formal")" = "$rollback_version"
test $((deadline - $(date +%s))) -gt 0
rm -f -- "$rollback_tmp"
if test "$owner_matches" = 1; then rm -f -- "$owner_file"; fi
printf '%s\n' 'rollback gate passed: active, listener, hash, exact v7.2.82 version'
