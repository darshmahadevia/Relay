# Reliability regression checks

## Process shutdown

Before the fix, `main` started `Server.Shutdown` in a goroutine. `ListenAndServe` returned when shutdown closed the listener. The main goroutine then exited while an upstream request was still running.

The executable regression test holds a backend response, sends `SIGTERM`, and verifies that the proxy stays alive. It then releases the backend, checks the complete response, and checks the process exit status.

The new test failed against the original executable:

```text
--- FAIL: TestShutdownDrainsActiveRequest (0.50s)
    main_test.go:102: proxy exited before active request completed: <nil>
```

The main goroutine waits for `Server.Shutdown` before closing idle upstream connections and exiting. The serving goroutine reports its result through a buffered channel. This also allows the terminal dashboard to run while the HTTP server accepts requests. The drain deadline remains ten seconds.

## Incomplete upstream responses

`httputil.ReverseProxy` aborts a connection when it cannot finish copying an upstream response. Before the fix, the circuit breaker treated a recorded `200` status as success even if the body copy failed. Logging rethrew `http.ErrAbortHandler` before counting or logging the request.

The new HTTP regression test sends five incomplete response bodies. It checks that clients see a read failure, the next request receives `503`, and the backend receives exactly five calls. It also checks error counts, aborted transfer counts, in-flight requests, and structured logs.

The initial test failed against the original handler:

```text
--- FAIL: TestTruncatedResponsesCountAsFailuresAndOpenCircuit (0.00s)
    streaming_test.go:47: Get "http://127.0.0.1:53290/truncated": EOF
```

The final test uses an 8 KiB partial body so clients also exercise failure after response headers arrive.

Two fixes were considered:

| Approach | Consequence |
| --- | --- |
| Wrap the transport and response body | Can distinguish upstream reads from downstream writes, but adds another error lifecycle and wrapper. |
| Track handler completion alongside status | Handles the reproduced abort through the existing handler boundary and preserves the standard library's connection abort. |

The implementation tracks handler completion. Every incomplete forwarding attempt counts as a circuit failure. Logging records the abort before rethrowing it. A request with both a `5xx` response and an aborted transfer counts once in `proxy_errors_total`.

The breaker also counts downstream write failures as failed forwarding attempts. It does not distinguish these from upstream failures.

## Rerun both regressions

```sh
go test -race -count=1 ./cmd/proxy ./internal/proxy \
	-run 'TestShutdownDrainsActiveRequest|TestTruncatedResponsesCountAsFailuresAndOpenCircuit'
```

The new tests passed after the fixes:

```text
ok  	github.com/darshm/reverseproxy/cmd/proxy	5.075s
ok  	github.com/darshm/reverseproxy/internal/proxy	4.132s
```

Subsequent runs also passed with the stronger partial-body assertion and Go 1.27.3. The shutdown test compiles and launches a separate executable. Its execution does not increase the parent process's statement coverage.
