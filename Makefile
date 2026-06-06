.PHONY: build test clean install

build:
	go build -o bin/forge.exe ./cmd/forge/

test:
	go test ./...

clean:
	rm -rf bin/

install: build
	cp bin/forge.exe ~/.harness/bin/forge.exe 2>/dev/null || mkdir -p ~/.harness/bin && cp bin/forge.exe ~/.harness/bin/forge.exe
	@echo "Installed to ~/.harness/bin/forge.exe"
