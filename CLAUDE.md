# Forge — AI 开发质量门禁引擎

## 项目结构

```
cmd/forge/main.go          — 入口，ldflags 注入版本
internal/
├── pipeline/               — 核心层：types, loader, dag, state, status, executor
├── rules/                  — 规则引擎：engine + 10 种规则类型评估器
├── knowledge/              — 跨项目经验库：store, scanner
├── hooks/                  — Hook 管理：embed, settings
├── skillgen/               — Skill 生成器
├── artifact/               — 外部集成：feishu
└── cli/                    — 命令层：root, init, gate, status, validate, knowledge, system
.goreleaser.yml             — 多平台编译 + Homebrew Cask + deb/rpm
.github/workflows/          — CI (三OS矩阵) + Release (tag 触发)
npm/                        — npm wrapper，postinstall 下载平台 binary
```

## 开发命令

```bash
go build ./...
go test ./... -count=1
go vet ./...
```

## 发版流程

```bash
# 1. 确认所有测试通过
go test ./... -count=1 && go vet ./...

# 2. 打 tag
git tag v0.1.0

# 3. 推送 tag（触发 GitHub Actions 自动构建发布）
git push origin v0.1.0
```

推送 tag 后 GitHub Actions 自动完成：
- 5 平台编译（linux/darwin/windows × amd64/arm64）
- 生成 checksums.txt
- 创建 GitHub Release（含所有 archive + checksums）
- 推送 Homebrew Cask 到 Harness/homebrew-tap

用户安装方式：
- `npm install -g @anthropic-ai/forge`
- `brew install Harness/tap/forge`
- 直接从 GitHub Releases 下载 binary

## 规则类型

file_contains, file_not_contains, json_equals, json_gte, json_lte, json_array_min_count, file_exists, all_gates_passed, custom_script, knowledge_check

未知类型 = FAIL（fail-safe）。新增规则类型只需在 `internal/rules/` 下新增文件并调用 `Register()`。
