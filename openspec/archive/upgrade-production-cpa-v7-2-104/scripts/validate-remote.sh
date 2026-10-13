#!/usr/bin/env bash
set -Eeuo pipefail

formal=/opt/cpa/bin/cli-proxy-api
config=/opt/cpa/config.yaml
expected_auth_dir=/opt/cpa/auths
formal_sha=6355d7424394f22293f9d9c8cb3b9ca0073734dc50e8b740bb2af5cea98aaf64
config_sha=73a8df5727b08c19c1e83e694ad9420d3135615c13ba79322719e188befbe10a
unit_path=/etc/systemd/system/cpa.service
unit_sha=9190014d8a8ab0331b6f188e4c98b0dfdc6049c714c145f531a888554c7b1111
formal_version='CLIProxyAPI Version: 7.2.104, Commit: c9417c8a, BuiltAt: 2026-07-28T08:36:09Z'

fail() {
  printf 'remote_validation_error=%s\n' "$1" >&2
  exit 1
}

version_line() {
  local output rc
  set +e
  output=$(timeout --foreground --signal=TERM --kill-after=2 5s "$1" -version 2>&1)
  rc=$?
  set -e
  test "$rc" = 2 || return "$rc"
  printf '%s\n' "$output" | awk '/^CLIProxyAPI Version: / {print; exit}'
}

listener_present() {
  local port=$1
  timeout --foreground --signal=TERM --kill-after=2 5s ss -ltnH \
    | awk -v port=":$port" '$4 ~ port "$" {found=1} END {exit !found}'
}

test "$(timeout 5 systemctl show -p ActiveState --value cpa.service)" = active || fail service_not_active
pid=$(timeout 5 systemctl show -p MainPID --value cpa.service)
test "$pid" -gt 0 || fail pid_missing
test "$(timeout 5 systemctl show -p NRestarts --value cpa.service)" = 0 || fail unexpected_restart_count
exec_start=$(timeout 5 systemctl show -p ExecStart --value cpa.service)
printf '%s' "$exec_start" | grep -Fq 'path=/opt/cpa/bin/cli-proxy-api' || fail exec_start_path_changed
printf '%s' "$exec_start" | grep -Fq 'argv[]=/opt/cpa/bin/cli-proxy-api -config /opt/cpa/config.yaml' || fail exec_start_args_changed
test "$(timeout 5 systemctl show -p FragmentPath --value cpa.service)" = "$unit_path" || fail unit_path_changed
test "$(timeout 5 sha256sum "$unit_path" | awk '{print $1}')" = "$unit_sha" || fail unit_sha_changed
test -z "$(timeout 5 systemctl show -p DropInPaths --value cpa.service)" || fail dropin_changed
test "$(timeout 5 readlink -f "/proc/$pid/exe")" = "$formal" || fail running_exe_path_changed
listener_present 8317 || fail port_8317_missing
listener_present 8080 || fail port_8080_missing
test "$(timeout 5 sha256sum "$formal" | awk '{print $1}')" = "$formal_sha" || fail formal_sha_mismatch
test "$(version_line "$formal")" = "$formal_version" || fail formal_version_mismatch
test "$(stat -c '%U:%G %a' "$formal")" = 'root:root 755' || fail formal_owner_mode_mismatch
test "$(timeout 5 sha256sum "$config" | awk '{print $1}')" = "$config_sha" || fail config_sha_mismatch

actual_auth_dir=$(awk -F: '
  /^[[:space:]]*auth-dir[[:space:]]*:/ {
    sub(/^[^:]*:[[:space:]]*/, "")
    gsub(/[[:space:]\047\042]/, "")
    print
    exit
  }
' "$config")
test "$actual_auth_dir" = "$expected_auth_dir" || fail auth_dir_changed

routing=$(awk -F: '/^[[:space:]]*strategy[[:space:]]*:/ {gsub(/[[:space:]\047\042]/, "", $2); print $2; exit}' "$config")
affinity=$(awk -F: '/^[[:space:]]*session-affinity[[:space:]]*:/ {gsub(/[[:space:]\047\042]/, "", $2); print $2; exit}' "$config")
test "$routing" = round-robin || fail routing_changed
test "$affinity" = false || fail session_affinity_changed
! grep -Eq '^[[:space:]]*plugins:[[:space:]]*$' "$config" || fail plugins_enabled
! grep -Eqi 'git.?token.?store|git.?store' "$config" || fail git_token_store_present
! grep -Eq 'plugins\.sync-revision|sync-revision:' "$config" || fail legacy_sync_revision_present

test "$(find "$expected_auth_dir" -maxdepth 1 -type f -name '*.json' | wc -l | tr -d ' ')" = 3 || fail auth_file_count_changed

printf 'remote_process_check=passed\n'
printf 'remote_main_pid_present=yes\n'
printf 'remote_restart_count=0\n'
printf 'remote_auth_file_count=3\n'
