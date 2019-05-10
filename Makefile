GOPATH ?= $(shell go env GOPATH)
BUILD_DIR ?= ./build
.PHONY: build test lint clean
.DEFAULT_GOAL := build

build:
	GO111MODULE=on go build -o $(BUILD_DIR)/tm-load-test ./cmd/tm-load-test/main.go
	GO111MODULE=on go build -o $(BUILD_DIR)/tm-outage-sim-server ./cmd/tm-outage-sim-server/main.go

test:
	GO111MODULE=on go test -cover -race ./...

$(GOPATH)/bin/golangci-lint:
	GO111MODULE=off go get -u github.com/golangci/golangci-lint/cmd/golangci-lint

lint: $(GOPATH)/bin/golangci-lint
	GO111MODULE=on $(GOPATH)/bin/golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR)
