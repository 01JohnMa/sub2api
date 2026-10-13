#!/usr/bin/env bash
set -Eeuo pipefail

window_minutes=${1:?window minutes required}
case "$window_minutes" in
  15|30) ;;
  *) printf 'observe_error=invalid_window\n' >&2; exit 2 ;;
esac

log=/var/log/nginx/access.log
cutoff=$(date -d "$window_minutes minutes ago" +%s)

gawk -v cutoff="$cutoff" -v window_minutes="$window_minutes" '
BEGIN {
  month["Jan"]=1; month["Feb"]=2; month["Mar"]=3; month["Apr"]=4;
  month["May"]=5; month["Jun"]=6; month["Jul"]=7; month["Aug"]=8;
  month["Sep"]=9; month["Oct"]=10; month["Nov"]=11; month["Dec"]=12;
}
function log_epoch(raw, parts) {
  sub(/^\[/, "", raw)
  split(raw, parts, /[\/:]/)
  return mktime(parts[3] " " month[parts[2]] " " parts[1] " " parts[4] " " parts[5] " " parts[6])
}
{
  if (log_epoch($4) < cutoff) next
  path=$7
  sub(/\?.*$/, "", path)
  if (path != "/v1/responses" && path != "/v1/chat/completions") next
  status=$9 + 0
  totals[path]++
  counts[path SUBSEP status]++
}
END {
  printf "{\"window_minutes\":%d,\"endpoints\":[", window_minutes
  sep=""
  for (key in counts) {
    split(key, value, SUBSEP)
    endpoint=value[1]
    status=value[2]
    printf "%s{\"endpoint\":\"%s\",\"status\":%d,\"count\":%d,\"total\":%d}", sep, endpoint, status, counts[key], totals[endpoint]
    sep=",";
  }
  printf "]}\n"
}
' "$log"

db=$(docker ps -q | while read -r id; do
  image=$(docker inspect -f '{{.Config.Image}}' "$id")
  if [[ "$image" == *postgres* ]]; then
    printf '%s\n' "$id"
    break
  fi
done)
test -n "$db"

docker exec -i "$db" sh -lc 'psql -X -qAt -v ON_ERROR_STOP=1 -v minutes="$1" -U "$POSTGRES_USER" -d "$POSTGRES_DB"' sh "$window_minutes" <<'SQL'
SELECT json_build_object(
  'window_minutes', :'minutes'::int,
  'success_rows', COUNT(*),
  'p95_duration_ms', COALESCE(ROUND(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms)::numeric, 2), 0),
  'p95_ttft_ms', COALESCE(ROUND((percentile_cont(0.95) WITHIN GROUP (ORDER BY first_token_ms) FILTER (WHERE first_token_ms > 0))::numeric, 2), 0)
)
FROM usage_logs
WHERE created_at >= NOW() - make_interval(mins => :'minutes'::int);

WITH scoped AS (
  SELECT
    CASE
      WHEN COALESCE(NULLIF(inbound_endpoint, ''), NULLIF(request_path, '')) IN ('/responses', '/v1/responses') THEN '/v1/responses'
      WHEN COALESCE(NULLIF(inbound_endpoint, ''), NULLIF(request_path, '')) IN ('/chat/completions', '/v1/chat/completions') THEN '/v1/chat/completions'
      ELSE COALESCE(NULLIF(inbound_endpoint, ''), NULLIF(request_path, ''), 'unknown')
    END AS endpoint,
    COALESCE(status_code, 0) AS status,
    md5(concat_ws('|',
      COALESCE(error_source, ''),
      COALESCE(error_type, ''),
      COALESCE(upstream_status_code::text, ''),
      COALESCE(provider_error_code, ''),
      COALESCE(provider_error_type, ''),
      COALESCE(network_error_type, '')
    )) AS signature
  FROM ops_error_logs
  WHERE created_at >= NOW() - make_interval(mins => :'minutes'::int)
    AND COALESCE(client_ip::text, '') NOT IN ('127.0.0.1', '::1')
), grouped AS (
  SELECT endpoint, status, signature, COUNT(*) AS count
  FROM scoped
  WHERE endpoint IN ('/v1/responses', '/v1/chat/completions')
  GROUP BY endpoint, status, signature
)
SELECT json_build_object(
  'window_minutes', :'minutes'::int,
  'signatures', COALESCE(
    json_agg(
      json_build_object(
        'endpoint', endpoint,
        'status', status,
        'signature', signature,
        'count', count
      ) ORDER BY endpoint, status, signature
    ),
    '[]'::json
  )
)
FROM grouped;
SQL
