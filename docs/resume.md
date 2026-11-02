# Resume entry

**Relay | Go HTTP reverse proxy**  
[GitHub repository](https://github.com/darshmahadevia/Relay)

- Built a Go HTTP reverse proxy with connection pooling, request deadlines, fixed-window rate limiting, and a circuit breaker with a single recovery probe.
- Developed a live terminal dashboard for request rates, p95 latency, and circuit status, with a recorded outage-and-recovery demo.
- Added HTTP and process-level regression tests for incomplete responses and graceful shutdown, plus race detection and terminal verification in CI.

## Supporting evidence

- [`internal/proxy`](../internal/proxy) contains the forwarding and resilience controls.
- [The demo recording](relay-demo.mp4) shows the live dashboard and failure sequence.
- [Reliability notes](reliability.md) document the defects, fixes, and regression checks.
- [The CI workflow](../.github/workflows/ci.yml) configures Go checks, demos, and terminal verification.
- [Benchmark results](benchmark.md) document the localhost workload and its limits.

HTTP forwarding uses `net/http/httputil.ReverseProxy`. The custom code adds resilience controls, request accounting, logging, server lifecycle, and the dashboard. Keep that distinction clear in an interview.
