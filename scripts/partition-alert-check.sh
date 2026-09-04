#!/usr/bin/env bash
#
# Fire the partition-runway alert on purpose, and confirm Prometheus notices.
#
# ADR-0028: a guard that has never fired is a guard nobody knows is wired up.
# The three ways this check could be fake, and how each is avoided:
#
#   * asserting the rule file parses  -> proves nothing about evaluation
#   * feeding Prometheus synthetic series -> a fixture standing in for reality
#   * asserting the job ran -> the failure we care about is a job that runs and
#     does nothing
#
# So this drops real future partitions from the real database, waits for the
# real exporter to observe it and the real Prometheus to evaluate the real
# rule, then puts the schema back.
#
# DESTRUCTIVE but self-repairing: it drops only partitions beyond the threshold
# and calls maintain_event_raw_partitions() to restore them.

set -euo pipefail

PROM="${PROM:-http://localhost:9090}"
ALERT="${ALERT:-PartitionRunwayLow}"
q() { docker compose exec -T postgres psql -U "${POSTGRES_USER:-sap}" -d "${POSTGRES_DB:-sap}" "$@"; }

alert_state() {
  curl -sf --max-time 10 "$PROM/api/v1/rules" \
    | python3 -c "
import sys, json
name = '$ALERT'
for g in json.load(sys.stdin)['data']['groups']:
    for r in g['rules']:
        if r.get('name') == name:
            print(r.get('state', 'unknown')); sys.exit(0)
print('absent')"
}

runway() { q -tAc "SELECT round(COALESCE(EXTRACT(EPOCH FROM (max(range_end)-now()))/86400.0,0)::numeric,1) FROM event_raw_partition"; }

restore() {
  echo "==> restoring the forward window"
  q -qc "SELECT maintain_event_raw_partitions(14, 400)" >/dev/null
  echo "    runway back to $(runway) days"
}
trap restore EXIT

echo "==> baseline"
echo "    runway    : $(runway) days"
echo "    alert     : $(alert_state)"

if [ "$(alert_state)" = "absent" ]; then
  echo "FAIL: rule $ALERT is not loaded in Prometheus" >&2
  exit 1
fi
if [ "$(alert_state)" != "inactive" ]; then
  echo "FAIL: $ALERT is already firing before we broke anything" >&2
  exit 1
fi

echo "==> dropping partitions beyond 5 days so real runway falls under the 7-day threshold"
q -qc "DO \$\$
DECLARE r record;
BEGIN
  FOR r IN SELECT partition_name FROM event_raw_partition
            WHERE range_start > now() + interval '5 days'
  LOOP EXECUTE format('DROP TABLE %I', r.partition_name); END LOOP;
END \$\$;" >/dev/null
echo "    runway now: $(runway) days"

echo "==> waiting for exporter scrape and rule evaluation"
fired=0
for i in $(seq 1 40); do
  s=$(alert_state)
  printf '\r    t+%-3ss  state=%-10s' "$((i*5))" "$s"
  if [ "$s" = "pending" ] || [ "$s" = "firing" ]; then fired=1; echo; break; fi
  sleep 5
done
echo

if [ "$fired" -eq 1 ]; then
  echo "ALERT FIRED — the guard is wired up"
else
  echo "FAIL: runway fell below the threshold and $ALERT never left inactive" >&2
  exit 1
fi
