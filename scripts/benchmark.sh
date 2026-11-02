#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
printf 'Date: '
date -u '+%Y-%m-%dT%H:%M:%SZ'
go version
uname -sm
if command -v sysctl >/dev/null 2>&1 && [ "$(uname -s)" = Darwin ]; then
	sysctl -n machdep.cpu.brand_string hw.ncpu hw.memsize
else
	getconf _NPROCESSORS_ONLN
fi
uptime
printf 'Payload: 1024 bytes; warmup: 20 requests; keep-alive: on; JSON logs: /dev/null\n'
printf 'GOMAXPROCS and client concurrency: %s; duration per sample: %s\n' "${BENCH_CPUS:-1,4}" "${BENCHTIME:-1s}"
for round in 1 2 3 4 5; do
	printf '\nRound %s of 5\n' "$round"
	go test -run '^$' -bench '^BenchmarkHTTP$' -benchmem -count=1 \
		-benchtime="${BENCHTIME:-1s}" -cpu="${BENCH_CPUS:-1,4}" ./internal/proxy
done
