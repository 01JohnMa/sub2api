#!/usr/bin/env bash

RUN_WITH_TIMEOUT_ACTIVE_PID=0
RUN_WITH_TIMEOUT_WATCHDOG_PID=0
CPA_LOG_BODY_CANARY='cpa-v104-body-canary-7f4d2e91'
CPA_LOG_SESSION_CANARY='cpa-v104-session-canary-9b8a1c36'

freeze_artifact_snapshot() {
  local source_dir=$1
  local expected_manifest_sha=$2
  local snapshot
  snapshot=$(mktemp -d /private/tmp/cpa-v104-artifacts.XXXXXX) || return 1
  case "$snapshot" in
    /private/tmp/cpa-v104-artifacts.*) ;;
    *) return 1 ;;
  esac
  if ! cp -a "$source_dir/." "$snapshot/"; then
    rm -rf -- "$snapshot"
    return 1
  fi
  local symlink_path manifest_line actual_manifest_sha
  if ! symlink_path=$(find "$snapshot" -xdev -type l -print -quit) \
    || test -n "$symlink_path" \
    || ! manifest_line=$(sha256sum "$snapshot/manifest.sha256"); then
    rm -rf -- "$snapshot"
    return 1
  fi
  actual_manifest_sha=${manifest_line%% *}
  if test "$actual_manifest_sha" != "$expected_manifest_sha" \
    || ! (cd "$snapshot" && sha256sum -c manifest.sha256 >/dev/null); then
    rm -rf -- "$snapshot"
    return 1
  fi
  local writable_path
  if ! chmod -R a-w "$snapshot" \
    || ! writable_path=$(find "$snapshot" -xdev \( -type f -o -type d \) -perm +0222 -print -quit) \
    || test -n "$writable_path"; then
    chmod -R u+w "$snapshot" 2>/dev/null || true
    rm -rf -- "$snapshot"
    return 1
  fi
  printf '%s\n' "$snapshot"
}

terminate_process_tree() {
  local root_pid=$1
  local signal=$2
  local child
  for child in $(pgrep -P "$root_pid" 2>/dev/null || true); do
    terminate_process_tree "$child" "$signal"
  done
  kill -"$signal" "$root_pid" 2>/dev/null || true
}

cancel_active_timeout_command() {
  local pid=${RUN_WITH_TIMEOUT_ACTIVE_PID:-0}
  local watchdog=${RUN_WITH_TIMEOUT_WATCHDOG_PID:-0}
  local _

  if test "$pid" -gt 0 && kill -0 "$pid" 2>/dev/null; then
    terminate_process_tree "$pid" TERM
    for _ in $(seq 1 20); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.1
    done
    if kill -0 "$pid" 2>/dev/null; then
      terminate_process_tree "$pid" KILL
    fi
    wait "$pid" 2>/dev/null || true
  fi

  if test "$watchdog" -gt 0; then
    kill "$watchdog" 2>/dev/null || true
    wait "$watchdog" 2>/dev/null || true
  fi

  RUN_WITH_TIMEOUT_ACTIVE_PID=0
  RUN_WITH_TIMEOUT_WATCHDOG_PID=0
}

run_with_timeout() {
  local seconds=$1
  shift

  "$@" <&0 &
  RUN_WITH_TIMEOUT_ACTIVE_PID=$!

  (
    sleep "$seconds"
    if kill -0 "$RUN_WITH_TIMEOUT_ACTIVE_PID" 2>/dev/null; then
      terminate_process_tree "$RUN_WITH_TIMEOUT_ACTIVE_PID" TERM
      sleep 2
      terminate_process_tree "$RUN_WITH_TIMEOUT_ACTIVE_PID" KILL
    fi
  ) &
  RUN_WITH_TIMEOUT_WATCHDOG_PID=$!

  local rc
  if wait "$RUN_WITH_TIMEOUT_ACTIVE_PID"; then
    rc=0
  else
    rc=$?
  fi

  kill "$RUN_WITH_TIMEOUT_WATCHDOG_PID" 2>/dev/null || true
  wait "$RUN_WITH_TIMEOUT_WATCHDOG_PID" 2>/dev/null || true
  RUN_WITH_TIMEOUT_ACTIVE_PID=0
  RUN_WITH_TIMEOUT_WATCHDOG_PID=0
  return "$rc"
}
