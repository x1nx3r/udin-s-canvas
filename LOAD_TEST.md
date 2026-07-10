# udin-canvas Load Test Writeup (v2 - SQLite WAL)
> **Date:** 2026-07-10 | **Host:** canvas.x1nx3r.dev | **Runtime:** Go binary via systemd

---

## Stack Under Test

| Layer | Technology |
|---|---|
| Server | Go binary (`udin-canvas`) — single process, systemd managed |
| Auth | Firebase Session Cookies (JWT, verified per-request locally) |
| Storage | **SQLite3 (cgo)** — `WAL` mode, `PRAGMA synchronous=NORMAL`, `MaxOpenConns(1)` |
| Infra | VPS, 128MB memory limit enforced by systemd |

---

## Test Tool

**k6** — running `k6_test.js` against the live production endpoints.

Each virtual user (VU) iteration exercises the full authenticated lifecycle:
1. `POST /draw/new` — create a drawing (redirects to editor)
2. `POST /api/draw/:id/save` — write canvas data (JSON payload)
3. `GET /api/draw/:id/data` — load canvas data
4. `DELETE /api/draw/:id` — delete drawing

---

## Round 1: 150 VU Stress Test

| Metric | Value |
|---|---|
| **Max VUs** | 150 (sustained for 50s) |
| **Throughput** | 146.41 req/s |
| **Total Requests** | 7,496 |
| **Error Rate** | **0.00% (0 failed)** |
| **Avg Response** | 51.51ms |
| **p(95) Response** | 143.11ms |
| **Max Response** | 1.33s |

#### Check breakdown
- `create redirected`: 100%
- `save success`: 100%
- `load success`: 100%
- `delete success`: 100%

> **Historical Context:** In v1 (Firestore), the system hit two distinct walls. At ~300 VUs, the sheer volume of reads/writes completely exhausted the **Firestore daily free tier quota**. When pushed to 500 VUs, the **Cloudflare Proxy (Orange Cloud)** buckled and began severing connections, resulting in a 46% error rate.
> 
> With **SQLite** and Cloudflare proxying disabled (raw Let's Encrypt via Caddy), 150 VUs barely woke the server up. Every single write succeeded.

---

### Round 1 System Profiling

During the peak of the 150 VU run, we captured live telemetry from the Go runtime using `net/http/pprof`. The results are staggering.

### Memory (Heap)
**Total In-Use Space: 5.88 MB**
Despite 150 concurrent users blasting JSON payloads, the server consumed less than 6 Megabytes of RAM. 
- The `bufio` readers/writers for HTTP connections consumed ~1 MB.
- Protobuf initializers for Firebase consumed ~1 MB.
- The `pprof` profiler itself consumed ~1.7 MB!
- **SQLite memory footprint was negligible.**

The 128MB systemd hard cap is functionally infinite for this architecture.

### CPU Utilization
We captured a 10-second CPU profile at maximum load. Total active CPU time was **1.58 seconds** (meaning the binary used only ~15.8% of a single CPU core).

Where did that 15% go?
1. **JWT Verification (31% of active CPU)**: `crypto/internal/.../rsa.VerifyPKCS1v15` and `montgomeryMul`. Verifying the Firebase Session Cookie cryptographic signature on every single request is currently the most computationally expensive thing the server does.
2. **CGO Overhead (25% of active CPU)**: `runtime.cgocall`. Crossing the C-boundary to talk to `mattn/go-sqlite3`. 
3. **SQLite Execution (<2% of active CPU)**: The actual database work (`database/sql.(*DB).conn`) took almost zero time.

---

## Architectural Reflection

### The Write Bottleneck is a Myth (For Now)
There was massive concern that configuring SQLite with `SetMaxOpenConns(1)` would cause a traffic jam of locked queries. 

At 146 requests per second (many of which were `INSERT`, `UPDATE`, and `DELETE` operations), the strictly serialized SQLite lock handled it flawlessly. The p(95) latency stayed at 143ms, and the max latency only spiked to 1.3s once. Because writes in WAL mode with `synchronous=NORMAL` take microseconds, the single connection was never tied up long enough to cause a timeout.

### The True Bottleneck: Cryptography
The CPU profile reveals that doing RSA math to verify the Firebase JWT on every request costs more CPU cycles than reading and writing to the database.

If we ever need to scale to thousands of requests per second, the first optimization will not be swapping SQLite for Postgres. It will be caching the Firebase JWT verification results in an LRU memory cache so we don't have to re-verify the RSA signature on subsequent requests from the same cookie.

## Round 2: 500 VU Breaking Point Test

We pushed the exact same setup that destroyed the Firestore architecture to see where SQLite would break. 

| Metric | Value |
|---|---|
| **Max VUs** | 500 (sustained for 30s) |
| **Throughput** | 455.98 req/s |
| **Total Requests** | 23,608 |
| **Error Rate** | **0.00% (0 failed)** |
| **Avg Response** | 112.48ms |
| **p(95) Response** | 330.67ms |
| **Max Response** | 2.5s |

#### Check breakdown
- `create redirected`: 100%
- `save success`: 100%
- `load success`: 100%
- `delete success`: 100%

> **The Verdict:** SQLite simply *did not break*. At 500 concurrent users doing 456 requests per second, the strictly serialized lock (`MaxOpenConns(1)`) handled every single write without a timeout. The p(95) latency increased to 330ms (from 143ms at 150 VUs), which is the expected queueing delay of hundreds of concurrent inserts waiting in line.

---

### Round 2 Peak System Profiling

During the peak of the 500 VU run, we captured a fresh round of telemetry:

### Memory (Heap)
**Total In-Use Space: 6.69 MB**
Tripling the concurrent connections from 150 to 500 increased the active memory footprint by exactly **0.81 MB**. The Go runtime and standard library `net/http` server are insanely efficient. The 128MB hard cap was never even remotely challenged.

### CPU Utilization
Active CPU time in a 10-second window jumped to **4.76 seconds** (~47.6% of a single CPU core).

The bottleneck distribution remained identical to the 150 VU run:
1. **JWT Verification (RSA Crypto):** Remained the #1 CPU consumer.
2. **CGO Overhead (`runtime.cgocall`):** 23% of active CPU.
3. **SQLite Execution:** ~7.6% of active CPU time.

---

## Conclusion

The migration to SQLite WAL mode was a resounding success that far exceeded expectations. 
- **Reliability:** 100% success rate at 500 concurrent users. (Firestore failed 46% of requests at this load).
- **Efficiency:** ~6.7MB of RAM and <50% of one CPU core at peak load.
- **Simplicity:** No external database quotas, no network latency to Google Cloud.

To go faster, the only optimizations left are internal: caching JWT validations and batching writes. But at 100% success rates, you don't need them yet.

---

## Round 3: 1,500 VU Meltdown Attempt

Unsatisfied with the server's survival, we tripled the concurrency again, aiming to exhaust socket descriptors, memory limits, or the single SQLite write lock. 

| Metric | Value |
|---|---|
| **Max VUs** | 1,500 (sustained for 40s) |
| **Throughput** | 553.03 req/s |
| **Total Requests** | 39,756 |
| **Error Rate** | **0.00% (0 failed)** |
| **Avg Response** | 1.01s |
| **p(95) Response** | 3.23s |
| **Max Response** | 14.49s |

#### Check breakdown
- `create redirected`: 100%
- `save success`: 100%
- `load success`: 100%
- `delete success`: 100%

> **The Verdict:** The system *refused to break*. At 1,500 concurrent users doing 553 requests per second, the single SQLite lock queued up writes flawlessly without a single timeout or "database is locked" error. The p(95) latency spiked to 3.23s (purely queueing delay), but the server answered every single request correctly.

---

### Round 3 Peak System Profiling

At the peak of 1,500 concurrent Virtual Users:

#### Memory (Heap vs OS)
**Go Heap In-Use:** 15.04 MB
**OS Peak (systemd):** 94.1 MB

While the Go heap only reported 15MB of in-use variables, the actual OS-level memory footprint (RSS) hit a peak of **94.1 MB** (out of the 128 MB limit). 

This 79MB gap between the Go heap and OS memory is due to:
1. SQLite's internal page cache and memory-mapped WAL files (managed by C, invisible to Go `pprof`).
2. Go runtime thread stacks (2,020 goroutines require memory for their stacks and OS threads).
3. Garbage collector overhead (Go holds onto freed memory before returning it to the OS).

We were only ~34MB away from the systemd OOM killer. 1,500 VUs is indeed the absolute physical ceiling for a 128MB limit. 

#### CPU Utilization
Total active CPU time jumped to **7.21 seconds** (meaning ~72.1% of a single CPU core was utilized). The single-core VPS still wasn't completely maxed out. 

The CPU profile showed:
1. **CGO Overhead (`runtime.cgocall`):** 24% of active CPU (processing the massive queue of SQLite writes).
2. **JWT Verification:** ~11% of active CPU.
3. **JSON Decoding:** ~5% (parsing huge incoming canvas payloads).

---

## Final Conclusion

The single-process Go binary backed by an embedded SQLite (WAL mode, `MaxOpenConns(1)`) has proven to be mathematically indestructible for this workload. 

It successfully processed 39,756 heavily authenticated write/read operations from 1,500 concurrent users without dropping a single request, staying well under 128MB of RAM and utilizing less than 1 CPU core. 

There is zero architectural need to move to a managed cloud database or introduce Redis. The bare-metal stack has won.
