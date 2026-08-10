.PHONY: build test lint vet clean install

BIN := bin/forgetmenot
VERSION := v0.1.0

# Static, dependency-free single binary (no cgo).
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/forgetmenot

test:
	go test ./...

vet:
	go vet ./...

lint: vet

install:
	go install ./cmd/forgetmenot

# Static, dependency-free single binary (no cgo).
static: build
	@echo "static binary: $(BIN)"
	@file $(BIN)

clean:
	rm -rf bin
