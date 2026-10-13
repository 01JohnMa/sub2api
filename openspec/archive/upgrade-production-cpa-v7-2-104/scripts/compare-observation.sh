#!/usr/bin/env bash
set -Eeuo pipefail

baseline_file=${1:?baseline file required}
post_file=${2:?post observation file required}
window=${3:?window required}
case "$window" in
  15|30) ;;
  *) printf 'observation_compare_error=invalid_window\n' >&2; exit 2 ;;
esac

baseline_nginx=$(sed -n "s/^nginx_${window}=//p" "$baseline_file")
baseline_usage=$(sed -n "s/^usage_${window}=//p" "$baseline_file")
baseline_errors=$(sed -n "s/^errors_${window}=//p" "$baseline_file")
post_nginx=$(sed -n '1p' "$post_file")
post_usage=$(sed -n '2p' "$post_file")
post_errors=$(sed -n '3p' "$post_file")

printf '%s' "$baseline_nginx" | jq -e . >/dev/null
printf '%s' "$baseline_usage" | jq -e . >/dev/null
printf '%s' "$baseline_errors" | jq -e . >/dev/null
printf '%s' "$post_nginx" | jq -e . >/dev/null
printf '%s' "$post_usage" | jq -e . >/dev/null
printf '%s' "$post_errors" | jq -e . >/dev/null

while IFS=$'\t' read -r endpoint status count total; do
  test -n "$endpoint" || continue
  base_count=$(printf '%s' "$baseline_nginx" | jq -r --arg endpoint "$endpoint" --argjson status "$status" '[.endpoints[] | select(.endpoint==$endpoint and .status==$status) | .count] | first // 0')
  base_total=$(printf '%s' "$baseline_nginx" | jq -r --arg endpoint "$endpoint" '[.endpoints[] | select(.endpoint==$endpoint) | .total] | first // 0')
  awk -v post_count="$count" -v post_total="$total" -v base_count="$base_count" -v base_total="$base_total" '
    BEGIN {
      if (post_total <= 0) exit 0
      post_rate = post_count / post_total
      if (base_count == 0 || base_total == 0) exit !(post_rate <= 0.01)
      base_rate = base_count / base_total
      threshold = base_rate + 0.02
      if (base_rate * 1.5 > threshold) threshold = base_rate * 1.5
      if (threshold > 1) threshold = 1
      exit !(post_rate <= threshold)
    }
  '
done < <(printf '%s' "$post_nginx" | jq -r '.endpoints[] | select(.status >= 500) | [.endpoint,.status,.count,.total] | @tsv')

while IFS=$'\t' read -r endpoint status signature count; do
  test -n "$endpoint" || continue
  test "$status" -ge 500 || continue
  base_count=$(printf '%s' "$baseline_errors" | jq -r --arg endpoint "$endpoint" --argjson status "$status" --arg signature "$signature" '[.signatures[] | select(.endpoint==$endpoint and .status==$status and .signature==$signature) | .count] | first // 0')
  post_total=$(printf '%s' "$post_nginx" | jq -r --arg endpoint "$endpoint" '[.endpoints[] | select(.endpoint==$endpoint) | .total] | first // 0')
  base_total=$(printf '%s' "$baseline_nginx" | jq -r --arg endpoint "$endpoint" '[.endpoints[] | select(.endpoint==$endpoint) | .total] | first // 0')
  awk -v post_count="$count" -v post_total="$post_total" -v base_count="$base_count" -v base_total="$base_total" '
    BEGIN {
      if (post_total <= 0) exit 1
      post_rate = post_count / post_total
      if (base_count == 0 || base_total == 0) exit !(post_rate <= 0.01)
      base_rate = base_count / base_total
      threshold = base_rate + 0.02
      if (base_rate * 1.5 > threshold) threshold = base_rate * 1.5
      if (threshold > 1) threshold = 1
      exit !(post_rate <= threshold)
    }
  '
done < <(printf '%s' "$post_errors" | jq -r '.signatures[] | [.endpoint,.status,.signature,.count] | @tsv')

while IFS=$'\t' read -r endpoint status count total; do
  test -n "$endpoint" || continue
  sig_total=$(printf '%s' "$post_errors" | jq -r --arg endpoint "$endpoint" --argjson status "$status" '[.signatures[] | select(.endpoint==$endpoint and .status==$status and .status >= 500) | .count] | add // 0')
  unclassified=$((count - sig_total))
  test "$unclassified" -ge 0 || exit 1
  awk -v unclassified="$unclassified" -v total="$total" 'BEGIN {exit !(total > 0 && unclassified / total <= 0.01)}'
done < <(printf '%s' "$post_nginx" | jq -r '.endpoints[] | select(.status >= 500) | [.endpoint,.status,.count,.total] | @tsv')

baseline_p95=$(printf '%s' "$baseline_usage" | jq -r '.p95_duration_ms')
baseline_ttft=$(printf '%s' "$baseline_usage" | jq -r '.p95_ttft_ms')
post_p95=$(printf '%s' "$post_usage" | jq -r '.p95_duration_ms')
post_ttft=$(printf '%s' "$post_usage" | jq -r '.p95_ttft_ms')
awk -v baseline="$baseline_p95" -v post="$post_p95" 'BEGIN {exit !(baseline > 0 && post <= baseline * 1.2)}'
awk -v baseline="$baseline_ttft" -v post="$post_ttft" 'BEGIN {exit !(baseline > 0 && post <= baseline * 1.2)}'

sample=$(printf '%s' "$post_nginx" | jq '[.endpoints | group_by(.endpoint)[] | .[0].total] | add // 0')
printf 'observation_compare=passed\n'
printf 'observation_sample=%s\n' "$sample"
