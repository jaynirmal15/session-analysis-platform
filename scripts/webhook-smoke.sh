#!/usr/bin/env bash
#
# End-to-end check: drive the real LiveKit in compose with a real client, then
# assert the rows its webhooks should have produced.
#
# This exists because synthetic payloads only prove we can parse what we *think*
# LiveKit sends. The signature format, the field names, the event ordering and
# the clock semantics are things only LiveKit can tell us, and it tells us by
# sending an actual delivery.
#
# The client runs INSIDE the compose network. That matters on macOS and Windows:
# Docker Desktop has no host networking, so a client on the host cannot complete
# ICE with the server. Container to container, LiveKit advertises an address the
# peer can reach and media flows normally.

set -euo pipefail

ROOM="${ROOM:-smoke-$$}"
IDENTITY="${IDENTITY:-smoke-participant}"
PUBLISH_SECONDS="${PUBLISH_SECONDS:-6}"
NETWORK="${NETWORK:-session-analysis-platform_default}"
LK_IMAGE="${LK_IMAGE:-livekit/livekit-cli:v2.4.0}"
API_KEY="${LIVEKIT_API_KEY:-devkey}"
API_SECRET="${LIVEKIT_API_SECRET:-devsecretdevsecretdevsecretdevsecret}"

q() { docker compose exec -T postgres psql -U "${POSTGRES_USER:-sap}" -d "${POSTGRES_DB:-sap}" "$@"; }

echo "==> room=$ROOM identity=$IDENTITY"
echo "    before: event_raw=$(q -tAc 'SELECT count(*) FROM event_raw') participant_join=$(q -tAc 'SELECT count(*) FROM participant_join')"

echo "==> joining with a real client, publishing demo video"
docker run --rm --network "$NETWORK" \
  -e LIVEKIT_URL=ws://livekit:7880 \
  -e LIVEKIT_API_KEY="$API_KEY" \
  -e LIVEKIT_API_SECRET="$API_SECRET" \
  "$LK_IMAGE" room join --identity "$IDENTITY" --publish-demo "$ROOM" \
  >/dev/null 2>&1 &
CLIENT=$!

sleep "$PUBLISH_SECONDS"
kill "$CLIENT" 2>/dev/null || true
wait "$CLIENT" 2>/dev/null || true

echo "==> waiting for the room to be reaped and its webhooks to land"
for _ in $(seq 1 40); do
  n=$(q -tAc "SELECT count(*) FROM event_raw WHERE room_name='$ROOM' AND event_type='room_finished'")
  [ "$n" -gt 0 ] && break
  sleep 3
done

echo
echo "==> events received, in backend-clock order"
q -c "SELECT event_type, participant_identity, track_sid IS NOT NULL AS has_track, occurred_at
        FROM event_raw WHERE room_name = '$ROOM' ORDER BY occurred_at, event_type;"

echo "==> joins produced"
q -c "SELECT participant_identity, participant_sid, ended_at IS NULL AS still_open,
             end_reason, ended_at - started_at AS duration
        FROM participant_join WHERE room_name = '$ROOM';"

fail=0
atleast() {
  if [ "$3" -ge "$2" ]; then printf '    ok   %-32s %s\n' "$1" "$3"
  else printf '    FAIL %-32s got %s want >= %s\n' "$1" "$3" "$2"; fail=1; fi
}
exact() {
  if [ "$2" = "$3" ]; then printf '    ok   %-32s %s\n' "$1" "$3"
  else printf '    FAIL %-32s got %s want %s\n' "$1" "$3" "$2"; fail=1; fi
}

echo
echo "==> assertions"
for t in room_started participant_joined track_published participant_left room_finished; do
  atleast "$t" 1 "$(q -tAc "SELECT count(*) FROM event_raw WHERE room_name='$ROOM' AND event_type='$t'")"
done
exact "payload retained"        0 "$(q -tAc "SELECT count(*) FROM event_raw WHERE room_name='$ROOM' AND payload='{}'::jsonb")"
exact "no egress/ingress stored" 0 "$(q -tAc "SELECT count(*) FROM event_raw WHERE room_name='$ROOM' AND (event_type LIKE 'egress%' OR event_type LIKE 'ingress%')")"
exact "one join"                1 "$(q -tAc "SELECT count(*) FROM participant_join WHERE room_name='$ROOM'")"
exact "join closed"             0 "$(q -tAc "SELECT count(*) FROM participant_join WHERE room_name='$ROOM' AND ended_at IS NULL")"
exact "closed by observed end"  0 "$(q -tAc "SELECT count(*) FROM participant_join WHERE room_name='$ROOM' AND end_reason IS NULL")"
exact "provenance recorded"     0 "$(q -tAc "SELECT count(*) FROM participant_join WHERE room_name='$ROOM' AND (started_event_id IS NULL OR ended_event_id IS NULL)")"

echo
if [ "$fail" -eq 0 ]; then echo "SMOKE PASSED"; else echo "SMOKE FAILED"; exit 1; fi
