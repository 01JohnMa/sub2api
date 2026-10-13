#!/usr/bin/env bash
set -Eeuo pipefail

marker=${1:?marker required}
db=$(docker ps -q | while read -r id; do
  image=$(docker inspect -f '{{.Config.Image}}' "$id")
  if [[ "$image" == *postgres* ]]; then
    printf '%s\n' "$id"
    break
  fi
done)
test -n "$db"

docker exec -i "$db" sh -lc 'psql -X -qAt -v ON_ERROR_STOP=1 -v marker="$1" -U "$POSTGRES_USER" -d "$POSTGRES_DB"' sh "$marker" <<'SQL'
WITH matched AS (
  SELECT *
  FROM usage_logs
  WHERE user_agent = :'marker'
    AND created_at >= NOW() - INTERVAL '10 minutes'
), billing AS (
  SELECT b.*
  FROM billing_usage_entries b
  JOIN matched m ON m.id = b.usage_log_id
), dedup AS (
  SELECT d.*
  FROM usage_billing_dedup d
  JOIN matched m ON m.request_id = d.request_id AND m.api_key_id = d.api_key_id
)
SELECT json_build_object(
  'usage_rows', (SELECT COUNT(*) FROM matched),
  'distinct_request_ids', (SELECT COUNT(DISTINCT request_id) FROM matched WHERE request_id IS NOT NULL),
  'legacy_billing_rows', (SELECT COUNT(*) FROM billing),
  'dedup_rows', (SELECT COUNT(*) FROM dedup),
  'input_tokens', (SELECT COALESCE(SUM(input_tokens), 0) FROM matched),
  'cached_tokens', (SELECT COALESCE(SUM(cache_read_tokens), 0) FROM matched),
  'output_tokens', (SELECT COALESCE(SUM(output_tokens), 0) FROM matched),
  'negative_cost_rows', (SELECT COUNT(*) FROM matched WHERE total_cost < 0 OR actual_cost < 0),
  'total_cost', (SELECT COALESCE(SUM(total_cost), 0) FROM matched),
  'actual_cost', (SELECT COALESCE(SUM(actual_cost), 0) FROM matched)
);
SQL
