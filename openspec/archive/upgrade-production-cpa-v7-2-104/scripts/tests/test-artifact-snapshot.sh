#!/usr/bin/env bash
set -Eeuo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source "$script_dir/../lib-local.sh"

tmpdir=$(mktemp -d /private/tmp/cpa-v104-artifact-snapshot-test.XXXXXX)
trap 'chmod -R u+w "$tmpdir" 2>/dev/null || true; rm -rf -- "$tmpdir"' EXIT

source_dir=$tmpdir/source
mkdir -p "$source_dir"
printf '%s\n' before > "$source_dir/payload.txt"
(cd "$source_dir" && sha256sum payload.txt > manifest.sha256)

snapshot=$(freeze_artifact_snapshot "$source_dir" "$(sha256sum "$source_dir/manifest.sha256" | awk '{print $1}')")
test ! -w "$snapshot/payload.txt"

printf '%s\n' after > "$source_dir/payload.txt"
test "$(sed -n '1p' "$snapshot/payload.txt")" = before
(cd "$snapshot" && sha256sum -c manifest.sha256 >/dev/null)

find_fakebin=$tmpdir/find-fakebin
mkdir "$find_fakebin"
printf '%s\n' '#!/usr/bin/env bash' 'exit 66' > "$find_fakebin/find"
chmod 0755 "$find_fakebin/find"
if PATH="$find_fakebin:$PATH" freeze_artifact_snapshot "$source_dir" "$(sha256sum "$source_dir/manifest.sha256" | awk '{print $1}')" >/dev/null; then
  exit 1
fi

untrusted=$tmpdir/untrusted-target
mkdir "$untrusted"
printf '%s\n' keep > "$untrusted/sentinel"
mktemp_fakebin=$tmpdir/mktemp-fakebin
mkdir "$mktemp_fakebin"
printf '%s\n' '#!/usr/bin/env bash' "printf '%s\\n' '$untrusted'" > "$mktemp_fakebin/mktemp"
chmod 0755 "$mktemp_fakebin/mktemp"
if PATH="$mktemp_fakebin:$PATH" freeze_artifact_snapshot "$source_dir" ignored >/dev/null; then
  exit 1
fi
test "$(sed -n '1p' "$untrusted/sentinel")" = keep

printf 'artifact_snapshot_mutation_isolated=passed\n'
