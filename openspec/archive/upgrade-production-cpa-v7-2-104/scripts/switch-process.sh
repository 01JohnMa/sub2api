set -Eeuo pipefail

run_id=${1:?run id required}
if [[ ! "$run_id" =~ ^cpa-v104-[A-Za-z0-9.-]+$ ]]; then
  printf 'switch_error=invalid_run_id\n' >&2
  exit 2
fi

formal=/opt/cpa/bin/cli-proxy-api
config=/opt/cpa/config.yaml
expected_auth_dir=/opt/cpa/auths
candidate=/opt/cpa/staging/v7.2.104/extracted/cli-proxy-api
rollback=/opt/cpa/backups/cpa-v7.2.82-20260729T111659Z/cli-proxy-api
config_sha=73a8df5727b08c19c1e83e694ad9420d3135615c13ba79322719e188befbe10a
unit_path=/etc/systemd/system/cpa.service
unit_sha=9190014d8a8ab0331b6f188e4c98b0dfdc6049c714c145f531a888554c7b1111
candidate_sha=6355d7424394f22293f9d9c8cb3b9ca0073734dc50e8b740bb2af5cea98aaf64
rollback_sha=e2ea31a127240f213d046567721839d725eeb4c136225eef139c6a46aa464b17
candidate_version='CLIProxyAPI Version: 7.2.104, Commit: c9417c8a, BuiltAt: 2026-07-28T08:36:09Z'
rollback_version='CLIProxyAPI Version: 7.2.82, Commit: f583414f, BuiltAt: 2026-07-17T08:17:41Z'
lock_dir=/run/cpa-v104-switch
lock_file=$lock_dir/lock
owner_file=$lock_dir/owner
install_tmp=/opt/cpa/bin/.cli-proxy-api.v7.2.104.$$
rollback_tmp=/opt/cpa/bin/.cli-proxy-api.v7.2.82.$$
switch_started=0
committed=0
recovery_deadline=0

umask 077
test ! -L "$lock_dir"
test -d "$lock_dir"
test "$(stat -c '%U:%G %a' "$lock_dir")" = 'root:root 700'
test ! -L "$lock_file"
test -f "$lock_file"
test "$(stat -c '%U:%G %a' "$lock_file")" = 'root:root 600'
exec 9<>"$lock_file"
flock -n 9 || {
  printf '%s\n' 'switch_error=another_switch_or_rollback_holds_lock' >&2
  exit 75
}
test -f "$owner_file" || {
  printf '%s\n' 'switch_error=owner_missing' >&2
  exit 76
}
test ! -L "$owner_file"
test "$(stat -c '%U:%G %a' "$owner_file")" = 'root:root 600'
test "$(sed -n '1p' "$owner_file")" = "$run_id" || {
  printf '%s\n' 'switch_error=owned_by_another_controller' >&2
  exit 76
}

remaining_seconds() {
  local deadline=$1
  local remaining=$((deadline - $(date +%s)))
  ((remaining > 0)) || return 124
  printf '%s\n' "$remaining"
}

run_until() {
  local deadline=$1
  local remaining
  shift
  remaining=$(remaining_seconds "$deadline") || return $?
  timeout --foreground --signal=TERM --kill-after=2 "${remaining}s" "$@"
}

run_process_group_until() {
  local deadline=$1
  local remaining
  shift
  remaining=$(remaining_seconds "$deadline") || return $?
  timeout --signal=TERM --kill-after=2 "${remaining}s" "$@"
}

sha256_until() {
  local deadline=$1
  local path=$2
  run_until "$deadline" sha256sum "$path" | awk '{print $1}'
}

version_line_until() {
  local deadline=$1
  local path=$2
  local output rc
  set +e
  output=$(run_until "$deadline" "$path" -version 2>&1)
  rc=$?
  set -e
  test "$rc" = 2
  printf '%s\n' "$output" | awk '/^CLIProxyAPI Version: / {print; exit}'
}

# BEGIN OAUTH SNAPSHOT FUNCTION
prepare_oauth_snapshot() {
  local deadline=$1
  local target=$2
  local owner=$3
  local group=$4

  run_process_group_until "$deadline" bash -s -- "$target" "$owner" "$group" <<'SNAPSHOT_SCRIPT'
set -Eeuo pipefail

target=$1
owner=$2
group=$3
manifest=$target/manifest.sha256
manifest_tmp=$target/.manifest.sha256.tmp

assert_no_find_match() {
  local output
  output=$(find "$@") || return $?
  test -z "$output"
}

test -d "$target"
test ! -L "$target"
assert_no_find_match "$target" -xdev -type l -print -quit
assert_no_find_match "$target" -xdev ! -type d ! -type f -print -quit

find "$target" -xdev \( -type d -o -type f \) -exec chown "$owner:$group" {} +
find "$target" -xdev -type d -exec chmod 0700 {} +
find "$target" -xdev -type f -exec chmod 0600 {} +

rm -f -- "$manifest_tmp"
(
  cd "$target"
  find . -xdev -type f ! -path './manifest.sha256' ! -path './.manifest.sha256.tmp' -printf '%P\0' \
    | sort -z \
    | xargs -0 -r sha256sum -- > "$manifest_tmp"
)
chown "$owner:$group" "$manifest_tmp"
chmod 0600 "$manifest_tmp"
mv -fT "$manifest_tmp" "$manifest"

expected_entries=$(find "$target" -xdev -type f ! -path "$manifest" | wc -l)
actual_entries=$(wc -l < "$manifest")
test "$expected_entries" -gt 0
test "$actual_entries" -eq "$expected_entries"
(
  cd "$target"
  sha256sum -c -- manifest.sha256 >/dev/null
)

assert_no_find_match "$target" -xdev \( ! -user "$owner" -o ! -group "$group" \) -print -quit
assert_no_find_match "$target" -xdev -type d ! -perm 0700 -print -quit
assert_no_find_match "$target" -xdev -type f ! -perm 0600 -print -quit
assert_no_find_match "$target" -xdev -type l -print -quit
assert_no_find_match "$target" -xdev ! -type d ! -type f -print -quit
SNAPSHOT_SCRIPT
}
# END OAUTH SNAPSHOT FUNCTION

listener_present_until() {
  local deadline=$1
  local remaining
  remaining=$(remaining_seconds "$deadline") || return $?
  timeout --foreground --signal=TERM --kill-after=2 "${remaining}s" ss -ltnH \
    | awk '$4 ~ /:8317$/ {found=1} END {exit !found}'
}

await_stopped_until() {
  local deadline=$1
  local state
  local pid
  while (( $(date +%s) < deadline )); do
    state=$(run_until "$deadline" systemctl show -p ActiveState --value cpa.service) || return $?
    pid=$(run_until "$deadline" systemctl show -p MainPID --value cpa.service) || return $?
    if test "$state" != active \
      && test "$pid" = 0 \
      && ! listener_present_until "$deadline"; then
      return 0
    fi
    sleep 1
  done
  return 124
}

await_active_until() {
  local deadline=$1
  local state
  while (( $(date +%s) < deadline )); do
    state=$(run_until "$deadline" systemctl show -p ActiveState --value cpa.service) || return $?
    if test "$state" = active && listener_present_until "$deadline"; then
      return 0
    fi
    sleep 1
  done
  return 124
}

stop_unit_until() {
  local deadline=$1
  local kill_deadline=$(($(date +%s) + 5))
  if ((kill_deadline > deadline)); then
    kill_deadline=$deadline
  fi
  run_until "$deadline" systemctl stop --no-block cpa.service
  if ((kill_deadline > $(date +%s))) && await_stopped_until "$kill_deadline"; then
    return 0
  fi
  run_until "$deadline" systemctl kill --kill-who=all --signal=KILL cpa.service
  await_stopped_until "$deadline"
}

start_unit_until() {
  local deadline=$1
  run_until "$deadline" systemctl start --no-block cpa.service
  await_active_until "$deadline"
}

rollback_binary() {
  local deadline=$1
  stop_unit_until "$deadline" || return $?
  run_until "$deadline" install -o root -g root -m 0755 "$rollback" "$rollback_tmp" || return $?
  test "$(sha256_until "$deadline" "$rollback_tmp")" = "$rollback_sha" || return 1
  run_until "$deadline" mv -fT "$rollback_tmp" "$formal" || return $?
  start_unit_until "$deadline" || return $?
  local restored_pid
  restored_pid=$(run_until "$deadline" systemctl show -p MainPID --value cpa.service) || return $?
  test "$restored_pid" -gt 0 || return 1
  test "$(run_until "$deadline" readlink -f "/proc/$restored_pid/exe")" = "$formal" || return 1
  test "$(sha256_until "$deadline" "$formal")" = "$rollback_sha" || return 1
  test "$(version_line_until "$deadline" "$formal")" = "$rollback_version" || return 1
  remaining_seconds "$deadline" >/dev/null
}

on_exit() {
  local rc=$?
  trap - EXIT INT TERM HUP
  if ((switch_started == 1 && committed == 0)); then
    if ! rollback_binary "$recovery_deadline"; then
      printf '%s\n' 'FATAL: automatic rollback did not pass active/listener/hash/version verification' >&2
      exit 70
    fi
  fi
  rm -f -- "$install_tmp" "$rollback_tmp"
  exit "$rc"
}

trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

preflight_deadline=$(($(date +%s) + 30))
test "$(run_until "$preflight_deadline" systemctl show -p ActiveState --value cpa.service)" = active
exec_start=$(run_until "$preflight_deadline" systemctl show -p ExecStart --value cpa.service)
printf '%s' "$exec_start" | grep -Fq 'path=/opt/cpa/bin/cli-proxy-api'
printf '%s' "$exec_start" | grep -Fq 'argv[]=/opt/cpa/bin/cli-proxy-api -config /opt/cpa/config.yaml'
preflight_pid=$(run_until "$preflight_deadline" systemctl show -p MainPID --value cpa.service)
test "$preflight_pid" -gt 0
test "$(run_until "$preflight_deadline" readlink -f "/proc/$preflight_pid/exe")" = "$formal"
test "$(sha256_until "$preflight_deadline" "$formal")" = "$rollback_sha"
test "$(version_line_until "$preflight_deadline" "$formal")" = "$rollback_version"
test "$(sha256_until "$preflight_deadline" "$candidate")" = "$candidate_sha"
test "$(version_line_until "$preflight_deadline" "$candidate")" = "$candidate_version"
test "$(sha256_until "$preflight_deadline" "$rollback")" = "$rollback_sha"
test "$(version_line_until "$preflight_deadline" "$rollback")" = "$rollback_version"
test "$(sha256_until "$preflight_deadline" "$config")" = "$config_sha"
test "$(run_until "$preflight_deadline" systemctl show -p FragmentPath --value cpa.service)" = "$unit_path"
test "$(sha256_until "$preflight_deadline" "$unit_path")" = "$unit_sha"
test -z "$(run_until "$preflight_deadline" systemctl show -p DropInPaths --value cpa.service)"
test "$(run_until "$preflight_deadline" systemctl show -p TimeoutStartUSec --value cpa.service)" = '1min 30s'
test "$(run_until "$preflight_deadline" systemctl show -p TimeoutStopUSec --value cpa.service)" = '1min 30s'
test "$(run_until "$preflight_deadline" systemctl show -p KillMode --value cpa.service)" = control-group
test "$(run_until "$preflight_deadline" systemctl show -p SendSIGKILL --value cpa.service)" = yes

actual_auth_dir=$(run_until "$preflight_deadline" awk -F: '
  /^[[:space:]]*auth-dir[[:space:]]*:/ {
    sub(/^[^:]*:[[:space:]]*/, "")
    gsub(/[[:space:]\047\042]/, "")
    print
    exit
  }
' "$config")
test "$actual_auth_dir" = "$expected_auth_dir"
remaining_seconds "$preflight_deadline" >/dev/null

snapshot_stamp=$(date -u +%Y%m%dT%H%M%SZ)-$$
oauth_snapshot=/opt/cpa/backups/cpa-v7.2.104-switch-oauth-$snapshot_stamp
oauth_snapshot_auth=$oauth_snapshot/auth
test ! -e "$oauth_snapshot"

outage_started=$(date +%s)
candidate_deadline=$((outage_started + 30))
recovery_deadline=$((outage_started + 60))
switch_started=1

stop_unit_until "$candidate_deadline"

run_until "$candidate_deadline" install -d -o root -g root -m 0700 "$oauth_snapshot"
run_until "$candidate_deadline" install -d -o root -g root -m 0700 "$oauth_snapshot_auth"
run_until "$candidate_deadline" cp -a "$expected_auth_dir/." "$oauth_snapshot_auth/"
prepare_oauth_snapshot "$candidate_deadline" "$oauth_snapshot" root root

run_until "$candidate_deadline" install -o root -g root -m 0755 "$candidate" "$install_tmp"
test "$(sha256_until "$candidate_deadline" "$install_tmp")" = "$candidate_sha"
run_until "$candidate_deadline" mv -fT "$install_tmp" "$formal"
start_unit_until "$candidate_deadline"
candidate_pid=$(run_until "$candidate_deadline" systemctl show -p MainPID --value cpa.service)
test "$candidate_pid" -gt 0
test "$(run_until "$candidate_deadline" readlink -f "/proc/$candidate_pid/exe")" = "$formal"
test "$(sha256_until "$candidate_deadline" "$formal")" = "$candidate_sha"
test "$(version_line_until "$candidate_deadline" "$formal")" = "$candidate_version"
remaining_seconds "$candidate_deadline" >/dev/null

committed=1
printf '%s\n' 'candidate process gate passed: active, listener, hash, exact version'
