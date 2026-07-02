.PHONY: build test clean install

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS = -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/forge.exe ./cmd/forge/

test:
	go test ./...

clean:
	rm -rf bin/

install: build
	cp bin/forge.exe ~/.harness/bin/forge.exe 2>/dev/null || mkdir -p ~/.harness/bin && cp bin/forge.exe ~/.harness/bin/forge.exe
	@echo "Installed to ~/.harness/bin/forge.exe"
