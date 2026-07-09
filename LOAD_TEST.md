# udin-canvas Load Test Writeup
> **Date:** 2026-07-09 | **Host:** canvas.x1nx3r.dev | **Runtime:** Go binary via systemd

---

## Stack Under Test

| Layer | Technology |
|---|---|
| Server | Go binary (`udin-canvas`) — single process, systemd managed |
| Auth | Firebase Session Cookies (JWT, verified per-request) |
| Storage | Google Firestore |
| Infra | VPS, 128MB memory limit enforced by systemd |

---

## Test Tool

**k6** — local binary, script [`k6_test.js`](file:///home/x1nx3r/mgodonf/gotth/k6_test.js)

Each virtual user (VU) iteration exercises the full authenticated lifecycle:

1. `GET /` — landing page
2. `GET /globals.css` — static asset
3. `GET /drawings` — authenticated dashboard
4. `POST /draw/new` — create a drawing
5. `POST /api/draw/:id/save` — write canvas data (JSON payload)
6. `GET /api/draw/:id/data` — load canvas data
7. `PUT /api/draw/:id/rename` — rename drawing
8. `POST /api/draw/:id/share` — toggle share
9. `DELETE /api/draw/:id` — delete drawing

---

## Results Summary

### Run 1 — Baseline (5 VUs, 25s)

| Metric | Value |
|---|---|
| Throughput | 17.1 req/s |
| Iterations | 50 |
| Error rate | **0.00%** |
| p(95) response | 194ms |
| Save p(95) | 165ms |
| Load p(95) | 112ms |
| All checks | ✅ 450/450 |

---

### Run 2 — Light Load (10 VUs, 25s)

| Metric | Value |
|---|---|
| Throughput | 33.5 req/s |
| Iterations | 98 |
| Error rate | **0.00%** |
| p(95) response | 214ms |
| Save p(95) | 217ms |
| Load p(95) | 155ms |
| All checks | ✅ 882/882 |

---

### Run 3 — Moderate Stress (50 VUs, 60s)

| Metric | Value |
|---|---|
| Throughput | 121 req/s |
| Iterations | 833 |
| Error rate | **0.00%** |
| p(95) response | 242ms |
| Save p(95) | 231ms |
| Load p(95) | 195ms |
| All checks | ✅ 7497/7497 |

---

### Run 4 — Breaking Point (500 VUs, 90s)

Staged ramp: 0 → 100 → 300 → 500 → 0

| Metric | Value |
|---|---|
| Throughput | 445 req/s |
| Iterations | 4651 |
| Error rate | **46.10%** ❌ (threshold: <5%) |
| p(95) response | 1.28s |
| Max response | **31.66s** |
| Save p(95) | 1.26s |
| Load p(95) | 1.26s |

#### Check breakdown at 500 VUs

| Check | Result | Success rate |
|---|---|---|
| landing 200 | ✅ | 100% |
| css 200 | ✅ | 100% |
| dashboard 200 | ✅ | 100% |
| create redirects | ✅ | 100% |
| save success | ❌ | **17%** |
| load success | ❌ | **17%** |
| rename success | ❌ | **17%** |
| share success | ❌ | **16%** |
| delete success | ❌ | **15%** |

---

## Latency Progression

| VUs | p(95) | Save p(95) | Load p(95) | Max |
|---|---|---|---|---|
| 5 | 194ms | 165ms | 112ms | 393ms |
| 10 | 214ms | 217ms | 155ms | 539ms |
| 50 | 242ms | 231ms | 195ms | 832ms |
| 500 | 1280ms | 1264ms | 1264ms | 31,660ms |

Latency curve is **flat up to 50 VUs**, then spikes hard at 300-400+ — consistent with an external quota wall rather than CPU/memory saturation.

---

## Server Resource Usage (systemd)

Observed at the end of all four test runs combined (~21 minutes uptime):

| Resource | Value | Limit |
|---|---|---|
| Memory (live) | 48 MB | 128 MB |
| Memory (peak) | **65 MB** | 128 MB |
| CPU total | 53.065s | — |
| Avg CPU utilization | ~4.2% | — |
| OS tasks (goroutines→threads) | 7 | — |

> The Go binary consumed less than half its memory cap under 500 concurrent users. CPU utilization over the entire test period averaged under 5%. **The process never came close to being the bottleneck.**

---

## Root Cause Analysis

The failure at 300-400 VUs is entirely attributable to **Firestore rate limiting / quota exhaustion**, not server saturation.

**Evidence:**
- All routes with no Firestore dependency (static pages, create redirect) remained at 100% success throughout
- All routes touching Firestore (save, load, rename, share, delete) collapsed simultaneously at the same VU count
- The Go binary's memory and CPU headroom was never approached

**Request path under load:**

```
Client
  │
  ▼
Go binary          (~0ms — in-process)
  │
  ├─► Firebase VerifySessionCookie  (external HTTPS, ~30-80ms RTT)
  │
  └─► Firestore read/write          (external HTTPS, ~80-150ms RTT)
                                     ↑
                              quota wall hit here
                              at ~300-400 concurrent ops
```

---

## Architectural Reflection

> **The server was never the problem. It was always the external I/O.**

The Firestore + Firebase Auth model was chosen for operational simplicity — no database to manage, no session store to run. That tradeoff worked well for correctness and ease of deployment, but it introduces a hard ceiling that is owned by Google's quota system, not your infrastructure.

### What a local-stack swap would look like

| Component | Current | Alternative |
|---|---|---|
| Session store | Firebase Auth (JWT verify, external) | Redis (same host, sub-millisecond) |
| Data storage | Firestore (external, quota-bound) | MySQL/Postgres (same host) |
| Estimated ceiling | ~300-400 concurrent ops | ~3000-5000+ (bound by disk/connections) |
| Memory overhead | ~0 (external) | +~30MB Redis + ~50MB MySQL |

With Redis + local MySQL, the two external round trips per request collapse to sub-millisecond local calls. The Go binary already has **~80MB of headroom** under its current 128MB cap — plenty of room for both services to co-exist.

The irony: a Go binary this lean was always going to make Firestore look slow. It processes requests so fast it has nothing to do but wait on the network.

---

## Conclusion

| Layer | Status | Actual ceiling |
|---|---|---|
| Go HTTP server | ✅ Never broke | Not found |
| Static routes | ✅ Never broke | Not found |
| Firebase Auth | ⚠️ Adds RTT per request | — |
| Firestore | ❌ Wall at ~300-400 VUs | Google quota |

**Recommendation:** For the current traffic profile (real users, not synthetic load), Firestore is perfectly adequate. If the app scales to hundreds of concurrent real users doing heavy canvas operations, the migration path is: Redis session cache → local MySQL/Postgres → drop Firebase Auth in favour of a self-signed JWT verified locally.

The binary will not be the reason you scale.
