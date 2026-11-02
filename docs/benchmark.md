# HTTP benchmark method

## Run the measurement

Use the latest patch release of Go 1.26 or Go 1.27. Close other CPU-intensive work before you run the benchmark.

```sh
make bench
```

The script prints the machine, Go version, load average, payload, concurrency, and duration. It runs five rounds. Each round measures the direct backend and the proxy at both concurrency settings. Every request uses HTTP over localhost.

To record another sample:

```sh
GOTOOLCHAIN=go1.27.3 make bench > /tmp/reverseproxy-benchmark.txt
```

To change the settings:

```sh
BENCH_CPUS=1,4 BENCHTIME=3s make bench
```

`BENCH_CPUS` sets both `GOMAXPROCS` and the concurrent client count. `BENCHTIME` sets Go's target duration for each measured sample. Calibration adds time to the run.

## Interpret the output

| Metric | Meaning |
| --- | --- |
| `ns/op` | Wall time divided by completed requests, including client work. This is not individual request latency under concurrency. |
| `req/s` | Completed requests divided by measured wall time. |
| `p50-ms`, `p95-ms` | Percentiles of individual request durations, through complete body read and validation. |
| `backend-requests` | Backend handler calls during the measured interval. Must match the iteration count. |
| `errors` | Requests with a transport error, wrong status, incomplete body, or wrong body. Any error fails the benchmark. |
| `B/op`, `allocs/op` | Allocations across the client, backend, proxy, and benchmark sampling code. |
| `MB/s` | Logical response payload throughput, not network wire bandwidth. |

Each sample warms the connection with twenty requests outside the timed interval. The response contains exactly 1,024 bytes. Keep-alive is enabled. The load generator allows at most 100 connections per host. The proxy uses its production transport defaults, including a 100-connection upstream cap. Rate limiting is disabled. JSON request logging runs with its output redirected to the null device.

The timed interval includes HTTP request creation, the complete response read, and status and byte validation. Sorting latency samples happens after the timer stops. The benchmark verifies backend calls so a cached response or skipped forwarding cannot inflate the result.

## Measured localhost sample

The [raw five-round sample](benchmarks/2026-11-02.txt) records Apple M4 hardware, ten logical CPUs, 16 GiB of memory, Darwin arm64, and Go 1.27.3. The client, backend, and proxy share one process. The machine also runs desktop applications and system services. These results describe this workload, not deployment capacity.

| Path | Concurrent clients | Median requests/s | Requests/s range | Median p50, ms | Median p95, ms | p95 range, ms |
| --- | --- | --- | --- | --- | --- | --- |
| Direct | 1 | 41,865 | 41,345 to 42,433 | 0.0233 | 0.0284 | 0.0283 to 0.0293 |
| Direct | 4 | 110,344 | 108,967 to 110,618 | 0.0313 | 0.0660 | 0.0650 to 0.0675 |
| Proxy | 1 | 18,061 | 17,146 to 18,466 | 0.0538 | 0.0650 | 0.0644 to 0.0813 |
| Proxy | 4 | 44,653 | 44,305 to 45,027 | 0.0750 | 0.2068 | 0.2053 to 0.2084 |

The table reports the median of the five per-run values. Ranges contain the smallest and largest per-run throughput and p95 values. Latency columns are medians of per-run percentiles, not percentiles pooled across all runs.

## Limits and profiling

The capacity verdict is inconclusive. This benchmark does not isolate the load generator, proxy, and backend on separate CPU resources. It does not establish peak throughput or performance under saturation.

A separate diagnostic run produced the [CPU profile summary](benchmarks/profile-2026-11-02.txt). Socket I/O and runtime scheduling dominate the sample. `syscall.rawsyscalln` accounts for 64.79% of sampled CPU, with `runtime.kevent` contributing 9.18%. This points to localhost I/O and scheduling as the main costs in this setup. The shared process prevents attribution of the capacity limit to the client or proxy alone.

The profile run is separate from the reported measurements. To collect a new profile:

```sh
go test -run '^$' -bench '^BenchmarkHTTP/proxy$' -benchtime=3s -cpu=4 \
	-cpuprofile=/tmp/reverseproxy.cpu -o /tmp/reverseproxy.test ./internal/proxy
go tool pprof -top /tmp/reverseproxy.test /tmp/reverseproxy.cpu
```

The workload has a fixed small body, warm connections, no backend processing delay, and no TLS. It does not exercise streaming, overload, error recovery, or a slow log destination. Do not turn its request rate into a claim about internet traffic or production service capacity.

For a resume, describe the benchmark and response validation. If you include a number, keep the payload, concurrency, hardware, and localhost qualification with it.
