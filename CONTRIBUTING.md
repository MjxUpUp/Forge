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
前缀决定发版 bump 类型（`scripts/release.js` 的 auto 推断依据），BREAKING CHANGE → major。

## 发版

只有维护者执行。流程与纪律见 [RELEASE.md](RELEASE.md)；标准路径：

```bash
node scripts/release.js          # auto 推断 bump，改版本 + 打 tag
git push origin main && git push origin v<版本>
```
