VERSION ?= 0.1.0
BIN_DIR := bin
BINARY := $(BIN_DIR)/junit-results
PKG := ./...
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test accept perf lint fmt vet clean install

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/junit-results

test:
	go test ./...

accept:
	go test -count=1 -run TestAcceptance ./internal/accept/ -v

perf:
	go test -tags perf -run TestPerf ./internal/accept/ -v

lint:
	golangci-lint run

fmt:
	gofmt -s -w .
	goimports -w .

vet:
	go vet ./...

clean:
	rm -rf $(BIN_DIR) dist/

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/junit-results
