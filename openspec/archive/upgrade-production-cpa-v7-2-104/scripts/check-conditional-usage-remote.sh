#!/usr/bin/env bash
set -Eeuo pipefail

window_hours=${1:-24}
test "$window_hours" = 24 || {
  printf 'conditional_usage_error=unsupported_window\n' >&2
  exit 2
}

db=$(docker ps -q | while read -r id; do
  image=$(docker inspect -f '{{.Config.Image}}' "$id")
  if [[ "$image" == *postgres* ]]; then
    printf '%s\n' "$id"
    break
  fi
done
)
test -n "$db"

row=$(docker exec -i "$db" sh -lc 'psql -X -qAt -v ON_ERROR_STOP=1 -F "|" -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
SELECT
  COUNT(*) FILTER (WHERE openai_ws_mode IS TRUE),
  COUNT(*) FILTER (WHERE lower(COALESCE(inbound_endpoint, '')) ~ '(websocket|sideband|webrtc|live)'),
  COUNT(*) FILTER (WHERE lower(COALESCE(user_agent, '')) ~ '(websocket|sideband|webrtc|codex.live)'),
  COUNT(*) FILTER (WHERE COALESCE(image_count, 0) > 0),
  COUNT(*) FILTER (WHERE COALESCE(video_count, 0) > 0)
FROM usage_logs
WHERE created_at >= NOW() - make_interval(hours => 24);
SQL
)
IFS='|' read -r websocket_rows endpoint_rows user_agent_rows image_rows video_rows <<< "$row"

printf 'conditional_window_hours=%s\n' "$window_hours"
printf 'websocket_rows=%s\n' "${websocket_rows:-0}"
printf 'special_endpoint_rows=%s\n' "${endpoint_rows:-0}"
printf 'special_user_agent_rows=%s\n' "${user_agent_rows:-0}"
printf 'image_rows=%s\n' "${image_rows:-0}"
printf 'video_rows=%s\n' "${video_rows:-0}"

if test "${websocket_rows:-0}" -gt 0 \
  || test "${endpoint_rows:-0}" -gt 0 \
  || test "${user_agent_rows:-0}" -gt 0 \
  || test "${image_rows:-0}" -gt 0 \
  || test "${video_rows:-0}" -gt 0; then
  printf 'conditional_usage=present\n'
  exit 78
fi

printf 'conditional_usage=absent\n'
