GOPATH ?= $(shell go env GOPATH)
BUILD_DIR ?= ./build
.PHONY: build build-tm-load-test build-tm-outage-sim-server \
	build-linux build-tm-load-test-linux build-tm-outage-sim-server-linux \
	test lint clean
.DEFAULT_GOAL := build

build: build-tm-load-test build-tm-outage-sim-server

build-tm-load-test:
	GO111MODULE=on go build -o $(BUILD_DIR)/tm-load-test ./cmd/tm-load-test/main.go

build-tm-outage-sim-server:
	GO111MODULE=on go build -o $(BUILD_DIR)/tm-outage-sim-server ./cmd/tm-outage-sim-server/main.go

build-linux: build-tm-load-test-linux build-tm-outage-sim-server-linux

build-tm-load-test-linux:
	GOOS=linux GOARCH=amd64 $(MAKE) build-tm-load-test

build-tm-outage-sim-server-linux:
	GOOS=linux GOARCH=amd64 $(MAKE) build-tm-outage-sim-server

test:
	GO111MODULE=on go test -cover -race ./...

$(GOPATH)/bin/golangci-lint:
	GO111MODULE=off go get -u github.com/golangci/golangci-lint/cmd/golangci-lint

lint: $(GOPATH)/bin/golangci-lint
	GO111MODULE=on $(GOPATH)/bin/golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR)
