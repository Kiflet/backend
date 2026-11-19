// The load test (docs/migration.md, phase 2, step 11; docs/deploy.md,
// "Load test"): phones syncing as the driver app does, against staging.
//
// live: every phone sends its newest position once a minute, the app's
// pace on the road. reconnect: after RECONNECT_AFTER, phones that were out
// of signal come back with two hours of positions each, one every 30
// seconds, as the app sends what it saved offline.
//
// The targets are the architecture's (section 15): a live sync answered
// within 300 ms at the 95th percentile, and no position waiting in
// tracking.ingest more than two minutes, which tests/load/report.sql
// reads from the database afterwards.
//
// The phones are from `kiflet seed-load` (make load-seed).
import http from "k6/http";
import { check } from "k6";
import { SharedArray } from "k6/data";
import exec from "k6/execution";
import { Trend } from "k6/metrics";

const BASE = __ENV.BASE_URL || "https://kiflet.localhost:8443";
const phones = new SharedArray("phones", () => JSON.parse(open(__ENV.PHONES || "./phones.json")));
const LIVE = Math.min(Number(__ENV.LIVE_PHONES || phones.length), phones.length);
const RECONNECT = Math.min(Number(__ENV.RECONNECT_PHONES || 3000), phones.length);
const DURATION = __ENV.DURATION || "15m";
const RECONNECT_AFTER = __ENV.RECONNECT_AFTER || "5m";
const BACKLOG = 2 * 60 * 60 * 1000;
const FIX_EVERY = 30 * 1000;

const reconnectDuration = new Trend("reconnect_duration", true);

export const options = {
  // Staging's certificates are from Caddy's own authority.
  insecureSkipTLSVerify: __ENV.INSECURE === "1",
  discardResponseBodies: false,
  scenarios: {
    live: {
      executor: "constant-arrival-rate",
      exec: "live",
      rate: LIVE,
      timeUnit: "1m",
      duration: DURATION,
      preAllocatedVUs: Math.max(10, Math.ceil(LIVE / 60)),
      maxVUs: Math.max(50, Math.ceil(LIVE / 10)),
    },
    reconnect: {
      executor: "shared-iterations",
      exec: "reconnect",
      iterations: RECONNECT,
      vus: Math.max(1, Math.min(200, RECONNECT)),
      startTime: RECONNECT_AFTER,
      maxDuration: DURATION,
    },
  },
  thresholds: {
    "http_req_duration{scenario:live}": ["p(95)<300"],
    http_req_failed: ["rate<0.01"],
    checks: ["rate>0.99"],
  },
};

function sync(p, n, locations) {
  return http.post(
    `${BASE}/api/v1/driver/sync`,
    JSON.stringify({
      device_id: `load-${n}`,
      app_version: "load-test",
      since: new Date(Date.now() - 60 * 1000).toISOString(),
      locations,
    }),
    { headers: { "Content-Type": "application/json", Authorization: `Bearer ${p.token}` } },
  );
}

function fix(p, n, id, at, k) {
  // A little way along the road from where the truck started.
  const step = (k % 600) * 0.0002;
  return {
    client_id: id,
    shipment_id: p.shipment_id,
    lat: p.lat - step,
    lng: p.lng + step,
    accuracy: 12,
    speed: 55,
    heading: 120,
    recorded_at: new Date(at).toISOString(),
  };
}

function stored(res, want) {
  try {
    return res.status === 200 && res.json("locations_stored") === want;
  } catch (e) {
    return false;
  }
}

// One phone's sync a minute, with the position it took just now.
export function live() {
  const i = exec.scenario.iterationInTest;
  const n = i % LIVE;
  const p = phones[n];
  const res = sync(p, n, [fix(p, n, `live-${i}`, Date.now(), Math.floor(i / LIVE))]);
  check(res, { "live: position taken": (r) => stored(r, 1) });
}

// A phone back in signal with two hours of positions, taken from the end
// of the list so that, with enough phones, they aren't the live ones.
export function reconnect() {
  const i = exec.scenario.iterationInTest;
  const n = phones.length - 1 - (i % RECONNECT);
  const p = phones[n];
  const now = Date.now();
  const locations = [];
  for (let k = 0, at = now - BACKLOG; at < now; k++, at += FIX_EVERY) {
    locations.push(fix(p, n, `back-${i}-${k}`, at, k));
  }
  const res = sync(p, n, locations);
  reconnectDuration.add(res.timings.duration);
  check(res, { "reconnect: positions taken": (r) => stored(r, locations.length) });
}
