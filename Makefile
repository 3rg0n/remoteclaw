BINARY := remoteclaw
MODULE := github.com/3rg0n/remoteclaw
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "-s -w \
	-X $(MODULE)/internal/config.Version=$(VERSION) \
	-X $(MODULE)/internal/config.Commit=$(COMMIT) \
	-X $(MODULE)/internal/config.Date=$(DATE)"

.PHONY: build test test-unit lint run clean build-all fmt vet release

build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/remoteclaw

run:
	go run $(LDFLAGS) ./cmd/remoteclaw run --config=config.yaml

test:
	go test -race -count=1 ./...

test-unit:
	go test -race -short -count=1 ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -rf bin/ dist/

build-all: clean
	GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY)-linux-amd64       ./cmd/remoteclaw
	GOOS=linux   GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY)-linux-arm64       ./cmd/remoteclaw
	GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY)-darwin-amd64      ./cmd/remoteclaw
	GOOS=darwin  GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY)-darwin-arm64      ./cmd/remoteclaw
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY)-windows-amd64.exe ./cmd/remoteclaw

release: clean build-all
	mkdir -p release
	cp dist/* release/
	cd release && sha256sum * > CHECKSUMS.txt 2>/dev/null || true
