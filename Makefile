# The Pico bundle is committed, so a fresh clone needs no network to build or
# test, and there is no generate step hiding in the gate. The generate target is
# for advancing the vendored Pico release; see README.md.
GO ?= go

.PHONY: all fmt vet build test race generate check clean

all: fmt vet build test

## fmt: rewrite sources with gofmt
fmt:
	gofmt -l -w $(shell $(GO) list -f '{{.Dir}}' ./...)

## vet: run go vet
vet:
	$(GO) vet ./...

## build: compile the package
build:
	$(GO) build ./...

## test: run the suite once, without caching
test:
	$(GO) test -count=1 ./...

## race: run the suite under the race detector (slow)
race:
	$(GO) test -race -count=1 ./...

## generate: fetch the Pico release named in pico.lock (network)
generate:
	$(GO) generate .

## check: verify the committed bundle against pico.lock (offline)
check:
	$(GO) run ./internal/fetchpico -check

## clean: nothing this Makefile creates; present for symmetry
clean:
