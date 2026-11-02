# Relay CLI reference

## Flags

| Flag | Default | Behavior |
| --- | --- | --- |
| `-listen` | `:8080` | Address on which Relay accepts connections. |
| `-backend` | `http://localhost:9000` | Fixed upstream URL. Accepts HTTP and HTTPS. |
| `-rate-limit` | `0` | Requests per second across the process. Zero disables the limiter. |
| `-upstream-timeout` | `15s` | Deadline for each upstream request. |
| `-ui` | `false` | Displays the terminal dashboard instead of JSON logs on stdout. |
| `-demo` | `false` | Starts the dashboard, a controlled backend, and generated HTTP traffic. |
| `-log-file` | empty | Also appends JSON logs to the specified file. |

Backend URLs must include a host and cannot contain credentials or a fragment. Negative rate limits and timeouts are invalid. A zero upstream timeout selects the 15-second default.

Demo mode sets its own backend, a 12-request-per-second quota, and a two-second upstream deadline. It defaults to `127.0.0.1:8080` and requires a loopback listening address.

## Local endpoints

Both endpoints accept `GET` and `HEAD`. Other methods receive `405`.

| Path | Response |
| --- | --- |
| `/healthz` | Liveness only. Returns `200` with `{"status":"ok"}` without checking the backend. |
| `/metrics` | Prometheus-compatible text exposition. |

All other paths go through the limiter and circuit breaker before forwarding. The standard library handles path joining, query strings, and hop-by-hop headers.

## Dashboard

The dashboard requires an interactive terminal with at least 76 columns and 28 rows. It responds to resizing and respects `NO_COLOR`.

Counters and circuit state come from the running proxy. The traffic graph retains up to 60 samples at roughly one-second intervals. Freezing the display leaves both request handling and sampling active.

The request table retains the last 200 completed requests. Its p95 uses logged request durations in milliseconds across that window, including rejected and aborted requests. Durations below one millisecond display `<1 ms`. An exclamation mark after the status identifies an aborted transfer.

| Key | Available in | Action |
| --- | --- | --- |
| `space` | Both modes | Freeze or resume the display. |
| `e` | Both modes | Toggle requests with a `4xx` or `5xx` status, plus aborted transfers. |
| `q`, `Ctrl+C` | Both modes | Quit and drain active requests. |
| `f` | Demo mode | Make the backend return `502`. |
| `r` | Demo mode | Restore the backend with a slower response to expose the recovery probe. |
| `b` | Demo mode | Generate a traffic burst. |
| `a` | Demo mode | Restart the automatic demo sequence. Counters remain cumulative. |

Dashboard mode keeps recent request events in memory. Add `-log-file` for a persistent JSON log. Service mode writes JSON logs to stdout.

## Metrics

| Name | Type | Meaning |
| --- | --- | --- |
| `proxy_requests_total` | Counter | Handled requests, including local endpoints and rejections. |
| `proxy_errors_total` | Counter | Requests with a `5xx` response or an aborted transfer. Counts each request once. |
| `proxy_aborted_total` | Counter | Transfers aborted before completion. |
| `proxy_in_flight` | Gauge | Requests currently forwarded upstream. |
| `proxy_rate_limited_total` | Counter | Requests rejected by the rate limiter. |
| `proxy_circuit_open_total` | Counter | Requests rejected by the circuit breaker. |
| `proxy_upstream_failures_total` | Counter | Upstream attempts with a `5xx` response, proxy error, or aborted transfer. |

A metrics scrape sees counters before that scrape completes. The circuit breaker treats aborted forwarding attempts as failures, including downstream write failures.

## Timeouts and shutdown

The upstream transport allows at most 100 connections per host. The HTTP server has a five-second header-read timeout, a 60-second idle timeout, and a 1 MiB header limit.

On `SIGINT` or `SIGTERM`, Relay stops accepting connections and waits up to ten seconds for active requests. A second signal can terminate the process immediately. Idle upstream connections close during cleanup.

The rate limiter uses a fixed one-second window. The circuit breaker opens after five consecutive failures, waits ten seconds, and admits one probe. A complete probe response below `500` closes the circuit. Both controls are process-local.

## Run the noninteractive demo

```sh
make demo
```

The demo checks forwarding, request IDs, liveness, timeouts, circuit opening and recovery, metrics, and rate limiting. It waits ten seconds for the recovery probe, then closes its servers and exits. It uses temporary localhost ports.

See the [recorded output](demo.txt). Regenerate it with `make demo > docs/demo.txt`.

## Record the dashboard

The recorder runs on macOS or Linux. It needs FFmpeg, Python 3, Pillow, pyte, and one of these fonts: Menlo, DejaVu Sans Mono, or Liberation Mono.

```sh
python3 -m venv .venv
.venv/bin/pip install Pillow pyte==0.8.2
make record PYTHON=.venv/bin/python
```

The recorder launches the compiled binary in a 120-column pseudoterminal. It captures real terminal output, exercises the freeze and error-filter controls, and verifies the circuit states. It quits through the keyboard after the automatic sequence.

It writes these artifacts:

- [`relay-demo.mp4`](relay-demo.mp4) is the video export.
- [`relay-dashboard.png`](relay-dashboard.png) is a frame from the recording.
- [`relay-demo.cast`](relay-demo.cast) is the source recording in asciinema v2 format.

Replay the source recording with `asciinema play docs/relay-demo.cast`.
