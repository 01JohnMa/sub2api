#!/usr/bin/env bash
set -Eeuo pipefail

declare -F prepare_oauth_snapshot >/dev/null

remaining_seconds() {
  local deadline=$1
  local remaining=$((deadline - $(date +%s)))
  test "$remaining" -gt 0
  printf '%s\n' "$remaining"
}

run_process_group_until() {
  local deadline=$1 remaining
  shift
  remaining=$(remaining_seconds "$deadline")
  timeout --signal=TERM --kill-after=2 "${remaining}s" "$@"
}

test_deadline() {
  printf '%s\n' "$(($(date +%s) + 20))"
}

tmpdir=$(mktemp -d /tmp/cpa-v104-snapshot-test.XXXXXX)
trap 'rm -rf -- "$tmpdir"' EXIT

source_dir=$tmpdir/source
snapshot=$tmpdir/snapshot
mkdir -p "$source_dir/nested dir"
printf 'root-data\n' > "$source_dir/root.json"
printf 'nested-data\n' > "$source_dir/nested dir/nested.json"
printf 'space-data\n' > "$source_dir/file with spaces.json"
chmod 0755 "$source_dir" "$source_dir/nested dir"
chmod 0644 "$source_dir/root.json" "$source_dir/nested dir/nested.json" "$source_dir/file with spaces.json"

install -d -m 0700 "$snapshot"
install -d -m 0700 "$snapshot/auth"
cp -a "$source_dir/." "$snapshot/auth/"
test "$(stat -c %a "$snapshot")" = 700
test "$(stat -c %a "$snapshot/auth")" = 755
prepare_oauth_snapshot "$(test_deadline)" "$snapshot" root root

test "$(find "$snapshot" -xdev -type d ! -perm 0700 | wc -l)" -eq 0
test "$(find "$snapshot" -xdev -type f ! -perm 0600 | wc -l)" -eq 0
test "$(find "$snapshot" -xdev \( ! -user root -o ! -group root \) | wc -l)" -eq 0
test "$(find "$snapshot" -xdev -type l | wc -l)" -eq 0
test "$(find "$snapshot" -xdev ! -type d ! -type f | wc -l)" -eq 0
test "$(find "$snapshot" -xdev -type f ! -path "$snapshot/manifest.sha256" | wc -l)" -eq 3
test "$(wc -l < "$snapshot/manifest.sha256")" -eq 3
(
  cd "$snapshot"
  sha256sum -c manifest.sha256 >/dev/null
)
printf 'oauth_snapshot_positive=passed\n'

printf 'tampered\n' >> "$snapshot/auth/root.json"
if (cd "$snapshot" && sha256sum -c manifest.sha256 >/dev/null 2>&1); then
  exit 1
fi
printf 'oauth_snapshot_tamper_detection=passed\n'

symlink_snapshot=$tmpdir/symlink-snapshot
install -d -m 0700 "$symlink_snapshot"
install -d -m 0755 "$symlink_snapshot/auth"
printf 'outside\n' > "$tmpdir/outside"
printf 'broad-data\n' > "$symlink_snapshot/auth/broad.json"
chmod 0644 "$symlink_snapshot/auth/broad.json"
ln -s "$tmpdir/outside" "$symlink_snapshot/auth/link"
if (prepare_oauth_snapshot "$(test_deadline)" "$symlink_snapshot" root root) >/dev/null 2>&1; then
  exit 1
fi
test "$(stat -c %a "$symlink_snapshot")" = 700
printf 'oauth_snapshot_symlink_rejected=passed\n'

special_snapshot=$tmpdir/special-snapshot
install -d -m 0700 "$special_snapshot"
install -d -m 0755 "$special_snapshot/auth"
mkfifo "$special_snapshot/auth/fifo"
if (prepare_oauth_snapshot "$(test_deadline)" "$special_snapshot" root root) >/dev/null 2>&1; then
  exit 1
fi
test "$(stat -c %a "$special_snapshot")" = 700
printf 'oauth_snapshot_special_file_rejected=passed\n'

empty_snapshot=$tmpdir/empty-snapshot
install -d -m 0700 "$empty_snapshot"
install -d -m 0755 "$empty_snapshot/auth"
if (prepare_oauth_snapshot "$(test_deadline)" "$empty_snapshot" root root) >/dev/null 2>&1; then
  exit 1
fi
test "$(stat -c %a "$empty_snapshot")" = 700
printf 'oauth_snapshot_empty_rejected=passed\n'

find_failure_snapshot=$tmpdir/find-failure-snapshot
install -d -m 0700 "$find_failure_snapshot"
install -d -m 0755 "$find_failure_snapshot/auth"
printf 'data\n' > "$find_failure_snapshot/auth/data.json"
wrapper_dir=$tmpdir/find-wrapper
mkdir "$wrapper_dir"
printf '#!/usr/bin/env bash\ncount=$(cat "$FIND_FAIL_COUNT")\ncount=$((count + 1))\nprintf "%%s\\n" "$count" > "$FIND_FAIL_COUNT"\nif test "$count" = 1; then exit 66; fi\nexec /usr/bin/find "$@"\n' > "$wrapper_dir/find"
chmod 0755 "$wrapper_dir/find"
printf '0\n' > "$tmpdir/find-fail-count"
if (PATH="$wrapper_dir:$PATH" FIND_FAIL_COUNT="$tmpdir/find-fail-count" \
  prepare_oauth_snapshot "$(test_deadline)" "$find_failure_snapshot" root root) >/dev/null 2>&1; then
  exit 1
fi
test "$(cat "$tmpdir/find-fail-count")" = 1
test "$(stat -c %a "$find_failure_snapshot")" = 700
printf 'oauth_snapshot_find_error_rejected=passed\n'

deadline_marker=$tmpdir/deadline-child-survived
deadline=$(($(date +%s) + 1))
set +e
run_process_group_until "$deadline" bash -c '(sleep 3; touch "$1") & wait' sh "$deadline_marker" >/dev/null 2>&1
deadline_rc=$?
set -e
test "$deadline_rc" = 124
sleep 3
test ! -e "$deadline_marker"
printf 'oauth_snapshot_deadline_kills_descendants=passed\n'
