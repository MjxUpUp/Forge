.PHONY: build test clean install premerge canary

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS = -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/forge.exe ./cmd/forge/

test:
	go test ./...

# canary：死代码金丝雀双零门禁（staticcheck + deadcode），CI（ci.yml build job
# 的 ubuntu 步骤）调用同一目标——工具版本与口径的唯一真相源在这里。
# 口径铁律：deadcode 必须带 -test（测试可达性算活代码：resetForTest 等测试
# 助手是合法存活），2026-09-06 dead-code-sweep 的「金丝雀双零」即此口径；
# 不带 -test 会把 19 个仅测试可达的函数误判为死代码。
# go run pkg@version 从模块缓存解析工具，不写入 go.mod、不进 vendor。
canary:
	go run honnef.co/go/tools/cmd/staticcheck@2026.1 ./...
	go run golang.org/x/tools/cmd/deadcode@v0.49.0 -test ./...

# premerge：合并到 main 前的本地兜底。退出码必须来自被验证的命令本身——
# 不要把 go test 接进管道再读 $?（管道的退出码是最后一个命令的，FAIL 会被
# head/grep 吞掉；2026-08-19 因此放过一次假绿）。覆盖：本机原生 build+vet、
# GOOS=windows/linux 交叉 build+vet（编译级平台差异；CGO_ENABLED=0——纯 Go CLI
# 交叉编译不得启用宿主 gcc，否则误编译 Go runtime 的 cgo 支撑文件：Windows 宿主
# 上报 gcc_linux_amd64.c 的 -Werror 告警或缺 grp.h 头，基线与分支同样失败，属
# 环境性既有缺陷，2026-08-29 收尾轮修）、全量测试（-race 对齐 CI）、gofmt。
# 行为级差异（路径分隔符等）本机原理上跑不出，靠分支 CI（ci.yml 对所有
# 分支 push 触发三平台）兜底——合并前确认分支 CI 绿。
premerge: canary
	@go build ./... && go vet ./... \
		&& CGO_ENABLED=0 GOOS=windows go build ./... && CGO_ENABLED=0 GOOS=windows go vet ./... \
		&& CGO_ENABLED=0 GOOS=linux go build ./... && CGO_ENABLED=0 GOOS=linux go vet ./... \
		&& go test ./... -count=1 -race
	@gofmt_out=$$(gofmt -l cmd internal skills); test -z "$$gofmt_out" || { echo "gofmt 未通过: $$gofmt_out"; exit 1; }
	@echo "premerge OK（行为级跨平台差异仍需分支 CI 确认）"

clean:
	rm -rf bin/

install: build
	cp bin/forge.exe ~/.harness/bin/forge.exe 2>/dev/null || mkdir -p ~/.harness/bin && cp bin/forge.exe ~/.harness/bin/forge.exe
	@echo "Installed to ~/.harness/bin/forge.exe"
