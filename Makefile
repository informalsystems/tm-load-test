GOPATH ?= $(shell go env GOPATH)
OUTPUT ?= build/tm-load-test
MAIN ?= cmd/tm-load-test/main.go
.PHONY: build test lint

build:
	GO111MODULE=on go build -o $(OUTPUT) $(MAIN)

test:
	GO111MODULE=on go test -cover -race ./...

$(GOPATH)/bin/golangci-lint:
	GO111MODULE=off go get -u github.com/golangci/golangci-lint/cmd/golangci-lint

lint: $(GOPATH)/bin/golangci-lint
	GO111MODULE=on $(GOPATH)/bin/golangci-lint run ./...

### Dev area

.DEFAULT_GOAL := build
.PHONY: protos clean


$(GOPATH)/bin/protoc-gen-gogoslick:
	GO111MODULE=off go get -u github.com/gogo/protobuf/...

protos: $(GOPATH)/bin/protoc-gen-gogoslick
	$(GOPATH)/bin/protoc-gen-gogoslick --gogoslick_out=pkg/loadtest/messages/ \
		-Ipkg/loadtest/messages/ \
		loadtest.proto

clean:
	rm -rf $(BUILD_DIR)
