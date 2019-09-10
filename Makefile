GOPATH ?= $(shell go env GOPATH)
BUILD_DIR ?= ./build
.PHONY: build build-tm-load-test build-tm-outage-sim-server \
	build-linux build-tm-load-test-linux build-tm-outage-sim-server-linux \
	test lint clean
.DEFAULT_GOAL := build
BUILD_FLAGS ?= -mod=readonly

build: build-tm-load-test build-tm-outage-sim-server

build-tm-load-test:
	go build $(BUILD_FLAGS) \
		-ldflags "-X github.com/interchainio/tm-load-test/pkg/loadtest.cliVersionCommitID=`git rev-parse --short HEAD`" \
		-o $(BUILD_DIR)/tm-load-test ./cmd/tm-load-test/main.go

build-tm-outage-sim-server:
	go build $(BUILD_FLAGS) -o $(BUILD_DIR)/tm-outage-sim-server ./cmd/tm-outage-sim-server/main.go

build-linux: build-tm-load-test-linux build-tm-outage-sim-server-linux

build-tm-load-test-linux:
	GOOS=linux GOARCH=amd64 $(MAKE) build-tm-load-test

build-tm-outage-sim-server-linux:
	GOOS=linux GOARCH=amd64 $(MAKE) build-tm-outage-sim-server

test:
	go test -cover -race ./...

$(GOPATH)/bin/golangci-lint:
	go get -u github.com/golangci/golangci-lint/cmd/golangci-lint

lint: $(GOPATH)/bin/golangci-lint
	$(GOPATH)/bin/golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR)
