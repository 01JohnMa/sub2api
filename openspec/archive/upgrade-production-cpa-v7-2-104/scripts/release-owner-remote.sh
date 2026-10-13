#!/usr/bin/env bash
set -Eeuo pipefail

run_id=${1:?run id required}
if [[ ! "$run_id" =~ ^cpa-v104-[A-Za-z0-9.-]+$ ]]; then
  printf 'owner_release_error=invalid_run_id\n' >&2
  exit 2
fi

lock_dir=/run/cpa-v104-switch
owner_file=$lock_dir/owner
lock_file=$lock_dir/lock
umask 077
test ! -L "$lock_dir"
test -d "$lock_dir"
test "$(stat -c '%U:%G %a' "$lock_dir")" = 'root:root 700'
test ! -L "$lock_file"
test -f "$lock_file"
test "$(stat -c '%U:%G %a' "$lock_file")" = 'root:root 600'
exec 9<>"$lock_file"
flock -w 65 9 || {
  printf 'owner_release_error=lock_not_released\n' >&2
  exit 75
}

if ! test -f "$owner_file"; then
  printf 'owner_release_error=owner_absent_ambiguous\n' >&2
  exit 77
fi
test ! -L "$owner_file"
test "$(stat -c '%U:%G %a' "$owner_file")" = 'root:root 600'
current_owner=$(sed -n '1p' "$owner_file")
if test "$current_owner" != "$run_id"; then
  printf 'owner_release_error=owned_by_another_controller\n' >&2
  exit 76
fi
rm -f -- "$owner_file"
printf 'owner_release=removed\n'
