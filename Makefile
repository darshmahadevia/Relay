.PHONY: build test vet fmt check demo bench dashboard record check-dashboard

PYTHON ?= python3

build:
	go build -trimpath -o bin/reverse-proxy ./cmd/proxy

test:
	go test -race -cover ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

check: test vet
	test -z "$$(gofmt -l cmd internal)"

demo:
	go run ./cmd/demo

bench:
	sh scripts/benchmark.sh

dashboard:
	go run ./cmd/proxy -demo

record: build
	$(PYTHON) scripts/record-demo.py

check-dashboard: build
	$(PYTHON) scripts/verify-dashboard.py
