GOPATH ?= $(shell go env GOPATH)
BUILD_DIR ?= $(CURDIR)/build
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

# Builds a Docker image called "tendermint/localnode", which is based on
# Tendermint Core. Takes the current system user and group ID as the user/group
# IDs for the tmuser user within the container so as to eliminate permissions
# issues when generating testnet files in the localnet target.
localnode:
	@docker build -f ./test/localnode/Dockerfile \
		--build-arg UID=$(shell id -u) \
		--build-arg GID=$(shell id -g) \
		-t tendermint/localnode:latest \
		./test/localnode/
.PHONY: localnode

localnet: localnode
	@if ! [ -f build/node0/config/genesis.json ]; then \
		mkdir -p build && \
		docker run \
			--rm \
			-v $(BUILD_DIR):/tendermint:Z \
			tendermint/localnode \
			testnet --config /etc/tendermint/config-template.toml --o . --starting-ip-address 192.168.10.2; \
	fi
.PHONY: localnet

localnet-start: localnet
	@docker-compose -f ./test/docker-compose.yml up -d
.PHONY: localnet-start

localnet-stop:
	@docker-compose -f ./test/docker-compose.yml down
.PHONY: localnet-stop

integration-test:
	@./test/integration-test.sh
.PHONY: integration-test

bench:
	go test -bench="Benchmark" -run="notests" ./...
.PHONY: bench

lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run
.PHONY: lint

clean:
	rm -rf $(BUILD_DIR)
.PHONY: clean

vulncheck:
	@go run golang.org/x/vuln/cmd/govulncheck@latest ./...
.PHONY: vulncheck
