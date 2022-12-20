GOPATH ?= $(shell go env GOPATH)
BUILD_DIR ?= ./build
.DEFAULT_GOAL := build
BUILD_FLAGS ?= -mod=readonly

build: build-tm-load-test build-tm-outage-sim-server
.PHONY: build

build-tm-load-test:
	@go build $(BUILD_FLAGS) \
		-ldflags "-X github.com/informalsystems/tm-load-test/pkg/loadtest.cliVersionCommitID=`git rev-parse --short HEAD`" \
		-o $(BUILD_DIR)/tm-load-test ./cmd/tm-load-test/main.go
.PHONY: build-tm-load-test

build-tm-outage-sim-server:
	@go build $(BUILD_FLAGS) -o $(BUILD_DIR)/tm-outage-sim-server ./cmd/tm-outage-sim-server/main.go
.PHONY: built-tm-outage-sim-server

build-linux: build-tm-load-test-linux build-tm-outage-sim-server-linux
.PHONY: build-linux

build-tm-load-test-linux:
	GOOS=linux GOARCH=amd64 $(MAKE) build-tm-load-test
.PHONY: build-tm-load-test-linux

build-tm-outage-sim-server-linux:
	GOOS=linux GOARCH=amd64 $(MAKE) build-tm-outage-sim-server
.PHONY: build-tm-outage-sim-server-linux

test:
	go test -cover -race ./...
.PHONY: test

bench:
	go test -bench="Benchmark" -run="notests" ./...
.PHONY: bench

lint:
	golangci-lint run ./...
.PHONY: lint

clean:
	rm -rf $(BUILD_DIR)
.PHONY: clean

vulncheck:
	@go run golang.org/x/vuln/cmd/govulncheck@latest ./...
.PHONY: vulncheck
