#!/usr/bin/env bash
# Starts deployments/clients/compose.yml and checks it is what Kiflet/web's
# and Kiflet/mobile's tests expect: the app ready, the demo company in it,
# and texts reaching the sandbox's phones. The images workflow runs it on
# what it is about to push; KIFLET_BACKEND_IMAGE, KIFLET_SANDBOX_IMAGE and
# KIFLET_TAG choose them, as for the compose file.
#
#   scripts/clients-check.sh            # the published main
#   KIFLET_TAG=sha-1234567 scripts/clients-check.sh
set -euo pipefail

compose=(docker compose -f "$(dirname "$0")/../deployments/clients/compose.yml")
app=http://localhost:${KIFLET_PORT:-8080}
sandbox=http://localhost:${SANDBOX_PORT:-8091}

cleanup() {
	status=$?
	if [ "$status" -ne 0 ]; then "${compose[@]}" logs --no-color --tail 100; fi
	"${compose[@]}" down -v >/dev/null 2>&1 || true
	exit "$status"
}
trap cleanup EXIT

"${compose[@]}" up -d --wait --wait-timeout 300

fail() {
	echo "clients-check: $*" >&2
	exit 1
}

curl -fsS -o /dev/null "$app/readyz" || fail "the app isn't ready"

# A demo driver signs in and has loads.
login=$(curl -fsS -X POST "$app/api/v1/driver/login" -H 'Content-Type: application/json' \
	-d '{"phone":"0912345601","pin":"2580","device_id":"clients-check"}') || fail "the demo driver can't sign in"
token=$(jq -r '.token // empty' <<<"$login")
[ -n "$token" ] || fail "no token in the sign-in's answer"
loads=$(curl -fsS -X POST "$app/api/v1/driver/sync" -H "Authorization: Bearer $token" \
	-H 'Content-Type: application/json' -d '{"device_id":"clients-check"}' | jq '.shipments | length')
[ "$loads" -gt 0 ] || fail "the demo driver has no loads"

# A texted code reaches the sandbox's phone.
curl -fsS -o /dev/null -X POST "$app/api/v1/driver/pin-reset/request" \
	-H 'Content-Type: application/json' -d '{"phone":"0912345602"}' || fail "asking for a PIN reset"
for _ in $(seq 30); do
	texts=$(curl -fsS "$sandbox/api/phones/0912345602" | jq '.texts | length')
	[ "$texts" -gt 0 ] && break
	sleep 1
done
[ "$texts" -gt 0 ] || fail "the PIN reset's text didn't reach the sandbox"

echo "clients-check: ready, the demo driver has $loads loads, and texts arrive"
