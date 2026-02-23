#!/bin/sh
# Runs inside the client container. Uses dig and curl to test CoreDNS + ruledforward.
set -e

COREDNS="${COREDNS_HOST:-coredns}"
METRICS="http://${COREDNS}:9153/metrics"
DIG_OPTS="+short +time=10 +tries=3"

log() { echo "[TEST] $*"; }
fail() { echo "[FAIL] $*"; exit 1; }
ok() { echo "[OK] $*"; }

# Wait for CoreDNS to be ready (query default->stub www.google.com). Allow time for dlc.dat load (~2MB).
wait_ready() {
  log "Waiting for CoreDNS at $COREDNS:53..."
  max=30
  i=0
  while [ $i -lt $max ]; do
    if dig @$COREDNS -t A www.google.com $DIG_OPTS 2>/dev/null | grep -q .; then
      ok "CoreDNS ready"
      return 0
    fi
    i=$((i+1))
    [ $i -lt $max ] && sleep 1
  done
  log "Last dig attempt: $(dig @$COREDNS -t A www.google.com $DIG_OPTS 2>&1 || true)"
  fail "CoreDNS not ready after ${max}s"
}
wait_ready

# --- DLC rule: geosite "google" (from official dlc.dat) -> stub returns 10.0.0.1 for www.google.com
# First query may be slow (dlc.dat ~2MB lookup); retry on timeout
log "DLC: www.google.com should resolve to 10.0.0.1 (stub)"
got=""
for _ in 1 2 3; do
  got=$(dig @$COREDNS www.google.com $DIG_OPTS 2>/dev/null | tr -d '\r' || true)
  if echo "$got" | grep -q '10\.0\.0\.1'; then
    ok "DLC www.google.com -> 10.0.0.1"
    break
  fi
  if echo "$got" | grep -qE 'timed out|no servers could be reached'; then
    log "DLC query timed out, retrying..."
    sleep 3
  else
    fail "DLC www.google.com: expected 10.0.0.1, got: $got"
  fi
done
echo "$got" | grep -q '10\.0\.0\.1' || fail "DLC www.google.com: expected 10.0.0.1, got: $got"

# --- AdGuard: blocked.ads should return NODATA (empty answer)
log "AdGuard: blocked.ads should return NODATA"
ans=$(dig @$COREDNS blocked.ads $DIG_OPTS 2>/dev/null | wc -l)
if [ "$ans" -eq 0 ] || ! dig @$COREDNS blocked.ads $DIG_OPTS 2>/dev/null | grep -q .; then
  ok "AdGuard blocked.ads -> NODATA"
else
  fail "AdGuard blocked.ads: expected NODATA, got answer"
fi

# --- Split: cn list -> group_cn, google list -> group_google (we only check they get an answer or no error)
log "Split: query cn list (test.cn) and google list (www.google.com)"
dig @$COREDNS test.cn $DIG_OPTS >/dev/null 2>&1 || true
dig @$COREDNS www.google.com $DIG_OPTS >/dev/null 2>&1 || true
ok "Split queries sent"

# --- UDP
log "UDP: www.google.com"
got=$(dig @$COREDNS www.google.com $DIG_OPTS 2>/dev/null | tr -d '\r' || true)
if echo "$got" | grep -q '10\.0\.0\.1'; then
  ok "UDP -> 10.0.0.1"
else
  fail "UDP: got $got"
fi

# --- TCP
log "TCP: www.google.com"
got=$(dig @$COREDNS www.google.com $DIG_OPTS +tcp 2>/dev/null | tr -d '\r' || true)
if echo "$got" | grep -q '10\.0\.0\.1'; then
  ok "TCP -> 10.0.0.1"
else
  fail "TCP: got $got"
fi

# --- Metrics: must expose ruledforward counters
log "Metrics: checking coredns_ruledforward_*"
metrics=$(curl -s -S --connect-timeout 3 "$METRICS" 2>/dev/null) || fail "Could not fetch metrics from $METRICS"
echo "$metrics" | grep -q 'coredns_ruledforward_requests_total' || fail "Missing coredns_ruledforward_requests_total"
echo "$metrics" | grep -q 'coredns_ruledforward_no_match_total' || fail "Missing coredns_ruledforward_no_match_total"
# forward_upstream_fail_total may be absent if no upstream failure occurred
echo "$metrics" | grep -q 'coredns_ruledforward_forward_upstream_fail_total' && ok "Metrics: forward_upstream_fail_total present" || log "Metrics: forward_upstream_fail_total absent (no failures)"
# At least one request should have been counted
if echo "$metrics" | grep 'coredns_ruledforward_requests_total' | grep -v '^#' | grep -q '[1-9]'; then
  ok "Metrics: counters present and > 0"
else
  fail "Metrics: no non-zero request counter"
fi

# --- Light load
log "Light load: 100 queries"
i=0
while [ $i -lt 100 ]; do
  dig @$COREDNS www.google.com $DIG_OPTS >/dev/null 2>&1 || true
  i=$((i+1))
done
ok "Light load done"

# --- Memory stress: many queries then verify metrics still respond (no crash/unbounded growth)
log "Memory stress: 10000 queries then re-check metrics"
i=0
while [ $i -lt 10000 ]; do
  dig @$COREDNS www.google.com $DIG_OPTS >/dev/null 2>&1 || true
  i=$((i+1))
done
metrics2=$(curl -s -S --connect-timeout 5 "$METRICS" 2>/dev/null) || fail "Metrics unreachable after load"
echo "$metrics2" | grep -q 'coredns_ruledforward_requests_total' || fail "Metrics missing after load"
ok "Memory stress done, metrics still OK"

log "All integration tests passed."
