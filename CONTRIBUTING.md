# Contributing

## 开发环境

- Go（版本见 `go.mod` 的 `go` 指令，CI 以它为准）
- Node.js 18+（仅发版脚本 `scripts/release.js` 需要）

## 提交前自检

```bash
go build ./...
go vet ./...
go test ./... -count=1 -race
gofmt -l cmd internal        # 应为空
node --test scripts/release.test.js   # 改了 scripts/release.js 时
```

CI（`.github/workflows/ci.yml`）在 PR 上跑同样内容，三平台矩阵。本地过了再提 PR。

## Commit 规范

Conventional Commits：`feat:` / `fix:` / `perf:` / `refactor:` / `docs:` / `chore:` / `test:` / `ci:`。
前缀决定 release-please 的 bump 类型：`feat:` → minor、`fix:` → patch、`!` / `BREAKING CHANGE:` → major；
`chore:` / `docs:` / `test:` / `ci:`（以及单独的 `perf:` / `refactor:`）不触发 Release PR。

### 长度与 body 结构

- 标题 ≤50 字符（中文 ≤25 字），小写开头、结尾不加句号——一行扫读能定位这个 commit 干了什么
- body 超过 3 行时用三段结构：**动机**（为什么改）→ **方案**（关键取舍）→ **验证**（实跑的命令与结果）
- **禁复述 diff**：body 写 diff 看不出来的信息（为什么、权衡、坑），diff 本体能看到的逐行描述不写
- 模板与档位判据见 `skills/doc-generator/references/template-pr.md` 与
  `skills/code-review-gate/references/rubric-docs.md`（PR 描述与 commit 共用三段结构）

## 发版

只有维护者执行。流程与纪律见 [RELEASE.md](RELEASE.md)；标准路径是合并 release-please 自动开出的
Release PR（`chore(main): release X.Y.Z`）——tag、GitHub Release、跨平台二进制、npm 包全部自动就绪，
无需本地手动步骤。`scripts/release.js` 已退役为 release-please 故障时的逃生舱（见 RELEASE.md「紧急手动路径」）。
