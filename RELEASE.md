# Forge 发布 Checklist

发版纪律——防止"本地过≠CI 过"和手动绕过 CI 发坏包。源自 v0.27.0/v0.27.1 教训：
v0.27.0 手动 `gh release` + `npm publish` 绕过了 failure 的 release.yml 当作完成，
cmd/forge 漏提交的雷拖到 v0.27.1 才爆。

## 发版必须走 release.yml（不手动绕过）

打 tag → `.github/workflows/release.yml` 自动跑 **test → goreleaser → npm** 三段强依赖链：

| job | 作用 | needs |
|-----|------|-------|
| **test** | `go test ./... -race` + `go vet` | （源头） |
| **goreleaser** | 跨平台二进制 + SBOM + cosign 签名 → GitHub Release | `test` |
| **npm** | 发 `@agent_forge/forge` + 5 平台子包到 npmjs.org（带 provenance） | `goreleaser` |

- **test** 失败 → goreleaser/npm 都不跑（needs 链由 `internal/ci/release_workflow_test.go` 沙盒守护）
- **goreleaser** 失败 → npm 不跑（npm 平台子包二进制来自 goreleaser 上传的 GitHub Release 产物，没产物则子包无二进制）
- **npm** 最后发：先发 5 平台子包（主包 optionalDependencies 依赖它们），再发主包；`NODE_AUTH_TOKEN` 走 `registry.npmjs.org`（华为云镜像缺新包会 404）
- **版本对账门禁**：npm job 先校验 tag == `npm/package.json` 的 version，不一致直接 fail——手动打 tag 时防"二进制是 tag 的、包版本号是 package.json 的"货不对板

```bash
# 标准发版（唯一推荐路径）
git tag vX.Y.Z
git push origin vX.Y.Z
# 等 release.yml 三 job 全绿 + GitHub Release 资产就绪 + npm 包发布
```

## 发布前自检（本地复现 CI 最小环境）

CI 是干净 clone，**本地工作区有文件 ≠ 仓库有文件**（cmd/forge 漏提交就是这么漏的：
.gitignore 裸名 `forge` 吞了 `cmd/forge/`，本地有文件所以本地过，CI 干净 clone 才暴露）。

```bash
# 干净 clone 验证（绝不依赖本地工作区已有文件）
git clone <remote> /tmp/forge-verify && cd /tmp/forge-verify
go build ./... && go test ./... -count=1 -race
git ls-files | grep -E 'cmd/forge|main\.go'   # 确认入口目录进库
```

## 紧急绕过（CI 暂坏需紧急发版）——必须留待办

手动 `gh release create` + `npm publish` 绕过 release.yml 时，绕过的是 **整个 workflow**，
`needs` 链根本没机会生效（沙盒验证无法覆盖手动行为）。此时：

1. **必须当场登记"CI 待修"待办**——v0.27.0 绕过 failure CI 当完成，是这次教训的根因
2. 绕过后第一时间修 CI，并补跑（重打 patch tag 走完整 release.yml 验证链路）
3. **绕过 ≠ CI 健康**：npm 包发出去了不代表发布链路 OK；CI 红着就是债

## 版本号规则

- 正常发版：patch/minor/major bump + 打 tag
- 发版后发现 bug：**升 patch 重发**，不 force-push 覆盖已发 tag
  - hazard-guard 会拦 force-push；且覆盖已发布 npm 包不可逆（registry 会缓存）
  - v0.27.1→v0.27.2 即此规则实例
