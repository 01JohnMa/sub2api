#!/usr/bin/env bash
set -Eeuo pipefail

since_epoch=${1:?validator start epoch required}
snapshot=${2:?snapshot path required}
body_canary=${3:?body canary required}
session_canary=${4:?session canary required}
snapshot_root=${CPA_LOG_SNAPSHOT_ROOT:-/run}
log_dir=${CPA_LOG_DIR:-/opt/cpa/logs}

fail() {
  printf 'log_validation_error=%s\n' "$1" >&2
  exit 1
}

# BEGIN LOG VALIDATION HELPERS
count_sensitive_matches() {
  local pattern=$1
  awk -v pattern="$pattern" 'tolower($0) ~ pattern {n++} END {print n+0}'
}

build_sensitive_pattern() {
  local body_canary=$1
  local session_canary=$2
  [[ "$body_canary" =~ ^cpa-v104-body-canary-[a-z0-9]+$ ]] || return 1
  [[ "$session_canary" =~ ^cpa-v104-session-canary-[a-z0-9]+$ ]] || return 1
  printf '%s\n' \
    '(^|[^[:alnum:]_-])(authorization|cookie|api[-_ ]?key|x-api[-_ ]?key|x_api_key|xapikey)(["[:space:]]*[:=]|[[:space:]]*$)'\
'|(^|[^[:alnum:]_-])bearer[[:space:]]+[a-z0-9]'\
'|(^|[^[:alnum:]_-])(resp|call|msg|sess)_[a-z0-9_-]{12,}'\
'|(^|[^[:alnum:]_-])x-cpa-trace-id(["[:space:]]*[:=]|[[:space:]]*$)'\
'|(^|[^[:alnum:]_-])20[0-9]{12}-[0-9a-f]{16}-[0-9a-f]{8}([^[:alnum:]_-]|$)'\
"|(^|[^[:alnum:]_-])($body_canary|$session_canary)([^[:alnum:]_-]|$)"
}

write_log_file_list() {
  local directory=$1
  local output=$2
  find "$directory" -maxdepth 1 -type f -print0 > "$output"
}
# END LOG VALIDATION HELPERS

case "$since_epoch" in
  *[!0-9]*|'') printf 'log_validation_error=invalid_epoch\n' >&2; exit 2 ;;
esac
case "$snapshot" in
  "$snapshot_root"/cpa-v104-log-offsets-*) ;;
  *)
    printf 'log_validation_error=invalid_snapshot_path\n' >&2
    exit 2
    ;;
esac
if [[ ! "$(basename -- "$snapshot")" =~ ^cpa-v104-log-offsets-[A-Za-z0-9.-]+$ ]]; then
  printf 'log_validation_error=invalid_snapshot_path\n' >&2
  exit 2
fi
test -f "$snapshot"
log_list=$(mktemp "$snapshot_root/cpa-v104-log-files.XXXXXX") || fail log_list_create_failed
chmod 0600 "$log_list" || fail log_list_mode_failed
trap 'rm -f -- "$snapshot" "$log_list"' EXIT

pattern=$(build_sensitive_pattern "$body_canary" "$session_canary") || fail invalid_privacy_canary
if ! journal_matches=$(journalctl -u cpa.service --since "@$since_epoch" --no-pager -q | count_sensitive_matches "$pattern"); then
  fail journal_read_failed
fi
file_matches=0
permission_changes=0
world_readable_new=0
new_files=0

if ! write_log_file_list "$log_dir" "$log_list"; then
  fail log_file_list_failed
fi

while IFS= read -r -d '' path; do
  old_record=$(awk -F '\t' -v path="$path" '$3 == path {print; exit}' "$snapshot")
  current_mode=$(stat -c '%a' "$path")
  current_size=$(stat -c '%s' "$path")
  if test -n "$old_record"; then
    old_mode=$(printf '%s\n' "$old_record" | awk -F '\t' '{print $1}')
    old_size=$(printf '%s\n' "$old_record" | awk -F '\t' '{print $2}')
    test "$current_mode" = "$old_mode" || permission_changes=$((permission_changes + 1))
  else
    old_size=0
    new_files=$((new_files + 1))
    if ((8#$current_mode & 4)); then
      world_readable_new=$((world_readable_new + 1))
    fi
  fi

  if test "$current_size" -lt "$old_size"; then
    old_size=0
  fi
  if test "$current_size" -gt "$old_size"; then
    if ! appended_matches=$(tail -c +$((old_size + 1)) "$path" | count_sensitive_matches "$pattern"); then
      fail log_tail_failed
    fi
    file_matches=$((file_matches + appended_matches))
  fi
done < "$log_list"

test "$journal_matches" = 0
test "$file_matches" = 0
test "$permission_changes" = 0
test "$world_readable_new" = 0

printf 'sensitive_metadata_matches=%s\n' "$journal_matches"
printf 'sensitive_file_append_matches=%s\n' "$file_matches"
printf 'log_permission_changes=%s\n' "$permission_changes"
printf 'new_log_files=%s\n' "$new_files"
printf 'world_readable_new_log_files=%s\n' "$world_readable_new"
