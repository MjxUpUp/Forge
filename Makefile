.PHONY: build test clean install premerge

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS = -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/forge.exe ./cmd/forge/

test:
	go test ./...

# premerge：合并到 main 前的本地兜底。退出码必须来自被验证的命令本身——
# 不要把 go test 接进管道再读 $?（管道的退出码是最后一个命令的，FAIL 会被
# head/grep 吞掉；2026-08-19 因此放过一次假绿）。覆盖：本机原生 build+vet、
# GOOS=windows/linux 交叉 build+vet（编译级平台差异）、全量测试（-race 对齐 CI）、
# gofmt。行为级差异（路径分隔符等）本机原理上跑不出，靠分支 CI（ci.yml 对所有
# 分支 push 触发三平台）兜底——合并前确认分支 CI 绿。
premerge:
	@go build ./... && go vet ./... \
		&& GOOS=windows go build ./... && GOOS=windows go vet ./... \
		&& GOOS=linux go build ./... && GOOS=linux go vet ./... \
		&& go test ./... -count=1 -race
	@gofmt_out=$$(gofmt -l cmd internal skills); test -z "$$gofmt_out" || { echo "gofmt 未通过: $$gofmt_out"; exit 1; }
	@echo "premerge OK（行为级跨平台差异仍需分支 CI 确认）"

clean:
	rm -rf bin/

install: build
	cp bin/forge.exe ~/.harness/bin/forge.exe 2>/dev/null || mkdir -p ~/.harness/bin && cp bin/forge.exe ~/.harness/bin/forge.exe
	@echo "Installed to ~/.harness/bin/forge.exe"
