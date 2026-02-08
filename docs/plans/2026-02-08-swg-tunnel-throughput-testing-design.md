# SWG Tunnel Throughput Testing — Design

## Goal

Measure throughput and CPU overhead of traffic flowing through the WireGuard tunnel in our SWG/SASE setup.

## Architecture

Three components, no Docker — just Go binaries and shell scripts on EC2 instances.

```
Your Laptop                   Content Server EC2          Tester EC2
┌──────────────┐              ┌─────────────────┐         ┌─────────────────┐
│ testron.sh   │──ssh────────▶│ content-server   │         │ oha (load gen)  │
│ (orchestrate)│──ssh────────▶│ cpu-collector    │◀──http──│ cpu-collector   │
└──────────────┘              └─────────────────┘         │ WireGuard client│
                                                          │ run-test.sh     │
                                                          └─────────────────┘
```

Traffic path under test:
```
oha (tester) → WireGuard tunnel → SWG server (wg0) → content-server
```

## Components

### 1. Content Server (Go binary)

Fast HTTP server with varied response types:

| Endpoint | Description |
|----------|-------------|
| `GET /json/small` | ~100 byte JSON |
| `GET /json/big` | ~1MB JSON |
| `GET /html` | HTML page (~50KB) |
| `GET /blob?size=10MB` | Binary data, configurable size |
| `GET /health` | Healthcheck |

Runs on port 8080 (HTTP) and 8443 (HTTPS).

### 2. CPU Collector (Go binary)

Lightweight sidecar that runs on both machines.

- Samples `/proc/stat` every second
- Ring buffer stores last N seconds (default 300s)
- Exposes:
  - `GET /cpu?last=30s` — per-second CPU % history
  - `GET /cpu/summary?last=30s` — avg/min/max

Runs on port 9100.

### 3. Tester Client

- **oha** (Rust) — HTTP load generator, low overhead
- **WireGuard** — connects through tunnel to SWG server
- **run-test.sh** — orchestrates a test run:
  1. Run oha against content server endpoints through tunnel
  2. Fetch CPU from local cpu-collector
  3. Fetch CPU from remote cpu-collector (content server side)
  4. Print combined report

### 4. testron.sh (runs from your laptop)

Single entry point using SSH aliases from your existing setup.

```bash
# Config file: testron.conf
CONTENT_SERVER=my-content-alias
TESTER=my-tester-alias
```

Commands:
- `./testron.sh setup` — SSH into both machines, install binaries, start services
- `./testron.sh run` — SSH into tester, run test, collect results
- `./testron.sh results` — fetch and display last test results

## Test Scenarios (Tunnel Only)

| Test | Endpoint | What It Measures |
|------|----------|-----------------|
| Small payload throughput | `/json/small` | Requests/sec through tunnel, overhead per request |
| Large payload throughput | `/blob?size=10MB` | Raw bandwidth through tunnel |
| Mixed realistic | `/html` + `/json/big` | Realistic web traffic pattern |
| Sustained load | `/blob?size=1MB` at high concurrency | Tunnel stability under load |

## Repo Structure

```
testron/
├── content-server/
│   └── main.go
├── cpu-collector/
│   └── main.go
├── tester/
│   └── run-test.sh
├── setup/
│   ├── setup-content-server.sh
│   └── setup-tester.sh
├── testron.sh
├── testron.conf.example
└── go.mod
```

## Deployment

- No Docker — bare Go binaries + shell scripts
- Setup scripts install dependencies (Go binaries from S3 or compile on machine, oha via cargo/binary)
- Triggered from laptop via SSH using existing aliases
- Results printed to terminal, optionally saved to file
