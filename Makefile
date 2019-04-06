GOPATH?=$(shell go env GOPATH)
GOBIN?=$(GOPATH)/bin
SRC_DIR?=$(GOPATH)/src/github.com/interchainio/tm-load-test
BUILD_DIR?=$(SRC_DIR)/build
.DEFAULT_GOAL := build
.PHONY: build-tm-outage-sim-server build-tm-outage-sim-server-linux \
	build build-linux \
	clean test lint \
	get-deps \
	protos

$(GOBIN)/dep:
	go get -u github.com/golang/dep/cmd/dep

$(GOBIN)/golangci-lint:
	go get -u github.com/golangci/golangci-lint/cmd/golangci-lint

get-deps: $(GOBIN)/dep
	dep ensure

get-linter: $(GOBIN)/golangci-lint

build-tm-outage-sim-server: get-deps
	go build -o $(BUILD_DIR)/tm-outage-sim-server \
		$(SRC_DIR)/cmd/tm-outage-sim-server/main.go

build-tm-outage-sim-server-linux: get-deps
	GOOS=linux GOARCH=amd64 \
		go build -o $(BUILD_DIR)/tm-outage-sim-server \
		$(SRC_DIR)/cmd/tm-outage-sim-server/main.go

build-tm-load-test: get-deps
	go build -o $(BUILD_DIR)/tm-load-test \
		$(SRC_DIR)/cmd/tm-load-test/main.go

build-tm-load-test-linux: get-deps
	GOOS=linux GOARCH=amd64 \
		go build -o $(BUILD_DIR)/tm-load-test \
		$(SRC_DIR)/cmd/tm-load-test/main.go

build: build-tm-outage-sim-server build-tm-load-test

build-linux: build-tm-outage-sim-server-linux build-tm-load-test-linux

protos: $(GOPATH)/bin/protoc-gen-gogoslick
	protoc --gogoslick_out=$(SRC_DIR)/pkg/loadtest/messages/ \
		-I$(SRC_DIR)/pkg/loadtest/messages/ \
		-I$(SRC_DIR)/vendor/ \
		loadtest.proto

$(GOPATH)/bin/protoc-gen-gogoslick:
	go get -u github.com/gogo/protobuf/...

lint: get-deps get-linter
	golangci-lint run ./...

test: get-deps
	go list ./... | grep -v /vendor/ | xargs go test -cover -race

clean:
	rm -rf $(BUILD_DIR)
