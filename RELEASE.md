# Forge 发布 Checklist

发版纪律——防止"本地过≠CI 过"和手动绕过 CI 发坏包。源自 v0.27.0/v0.27.1 教训：
v0.27.0 手动 `gh release` + `npm publish` 绕过了 failure 的 release.yml 当作完成，
cmd/forge 漏提交的雷拖到 v0.27.1 才爆。

## 标准发版：合并 Release PR（唯一推荐路径）

版本号 bump / changelog / 打 tag 由 [release-please](https://github.com/googleapis/release-please)
接管（`.github/workflows/release-please.yml` + `release-please-config.json` +
`.release-please-manifest.json`）：

1. `feat:`/`fix:` 合入 main → release-please 自动开（或更新）**Release PR**
   （`chore(main): release X.Y.Z`），内容是纯机械变更：`npm/package.json`、
   `.kimi-plugin/plugin.json`、`plugins/forge-dsh/package.json`、
   `.release-please-manifest.json` 的版本 bump + `CHANGELOG.md` 新章节
2. 确认后**合并 Release PR** → release-please 自动打 `vX.Y.Z` tag、建带 changelog
   正文的 GitHub Release
3. 同一 workflow 用 `workflow_dispatch` 在新 tag 上调度 `release.yml`（构建层零改动）

版本规则（Conventional Commits）：

- `feat:` → minor，`fix:` → patch，`!`/`BREAKING CHANGE:` → major
- `chore:`/`docs:`/`test:`/`ci:` 不触发 Release PR（与旧 release.js 的差异：perf/refactor
  也不再独立触发发版，攒到下次 feat/fix 一起发）
- 强制指定版本：给任意 commit 加 `Release-As: x.y.z` footer

**Token 双路径**：默认 `GITHUB_TOKEN`（零配置即可用）——它产生的事件不触发新
workflow run（GitHub 防递归），所以靠 workflow_dispatch 显式调度构建层。配置 secret
`RELEASE_PLEASE_TOKEN`（PAT）后自动升级：Release PR 上能跑 CI 检查、tag push 直接
触发 release.yml（dispatch 步自停，防双跑），无需改任何文件。

发版形状由 `internal/ci/release_please_test.go` 守卫：tag 形状必须 `v<semver>`（构建层
触发条件与 npm 资产 URL 都依赖）、extra-files 必须持续 bump 三个版本文件
（`npm/package.json`、`.kimi-plugin/plugin.json`、`plugins/forge-dsh/package.json`）、
`.release-please-manifest.json` 必须与 `npm/package.json` 同版本（手动发版不同步会被
测试拦下）。

`@agent_forge/forge-dsh`（DSH 插件）随主发布火车 lockstep 发版：版本号由 Release PR
经 extra-files 与根版本统一 bump（不手改，`TestReleasePleaseManifest_DshPluginTracksTrain`
守卫），release.yml 的 npm job 幂等发布到 npmjs.org。曾刻意独立演进，结果版本 bump
全靠手动 chore commit、漏 bump 时发布步静默 skip——2026-08 收回火车。

注：`plugins/forge-dsh/package-lock.json` 的根 version 字段**不做机制性同步**——
`npm ci` 只校验依赖树不校验根 version，npm publish 也不把 lockfile 打进 tarball，
该字段是纯本地开发元数据（下次 `npm install` 自然追上）。把它塞进 extra-files 需要
未验证的 jsonpath 写法（`packages[""]` 空字符串 key），不值得为化妆性字段冒险。

## 发版必须走 release.yml（不手动绕过）

Release PR 合并 → dispatch → `.github/workflows/release.yml` 跑 **test → goreleaser →
npm → npm-verify** 四段强依赖链：

| job | 作用 | needs |
|-----|------|-------|
| **test** | `go test ./... -race` + `go vet` + tag↔版本对账 | （源头） |
| **goreleaser** | 跨平台二进制 + SBOM + cosign 签名 → GitHub Release 资产 | `test` |
| **npm** | 发 `@agent_forge/forge` + 5 平台子包 + `@agent_forge/forge-dsh` 到 npmjs.org（带 provenance） | `goreleaser` |
| **npm-verify** | npm 装回并断言 `forge --version` == tag | `npm` |

- needs 链由 `internal/ci/release_workflow_test.go` 沙盒守护
- goreleaser `release.mode: keep-existing`：保留 release-please 的 changelog 正文，
  只往 Release 上挂二进制/SBOM/签名资产
- **版本对账门禁**（test job）：tag 必须等于 `npm/package.json` 与
  `plugins/forge-dsh/package.json` 的 version——Release PR 已保证一致，此门禁防手动
  打 tag 路径"二进制是 tag 的、包版本号是 package.json 的"货不对板
- **npm** 先发 5 平台子包（主包 optionalDependencies 依赖它们）再发主包；
  `NODE_AUTH_TOKEN` 走 `registry.npmjs.org`（华为云镜像缺新包会 404）

```bash
# 标准发版就是：在 GitHub 上合并 Release PR（chore(main): release X.Y.Z）
# 之后无需任何手动步骤——tag、GitHub Release、二进制、npm 包全部自动就绪
```

## 宿主插件是第二分发通道（发版 ≠ 生效）

kimi 等宿主插件 manifest（`.kimi-plugin/plugin.json`）随 tag 进 GitHub，但**用户机器上
的已装副本不自动更新**——含 hook 接线 / manifest 变更的发版，binary 升级 ≠ 行为生效，
用户须在宿主里更新插件（kimi 侧有 staleness advisory 在下个 prompt 提醒）。此类发版
在 commit body 写明"需更新宿主插件"，避免"发了版用户还报旧症状"（2026-08 kimi
skill-trigger manifest 接线修复实例：引擎修复已发版，插件 manifest 未更新，症状照旧）。

## 发布前自检（本地复现 CI 最小环境）

CI 是干净 clone，**本地工作区有文件 ≠ 仓库有文件**（cmd/forge 漏提交就是这么漏的：
.gitignore 裸名 `forge` 吞了 `cmd/forge/`，本地有文件所以本地过，CI 干净 clone 才暴露）。

```bash
# 干净 clone 验证（绝不依赖本地工作区已有文件）
git clone <remote> /tmp/forge-verify && cd /tmp/forge-verify
go build ./... && go test ./... -count=1 -race
git ls-files | grep -E 'cmd/forge|main\.go'   # 确认入口目录进库
```

## 紧急手动路径（release.js，已退役为逃生舱）

`scripts/release.js` 不再是标准路径，仅当 release-please 层本身故障时应急：

```bash
node scripts/release.js          # 照旧 bump + tag + commit
git push origin main && git push origin vX.Y.Z   # 手动 push 触发 release.yml
```

脚本会同步 bump `.release-please-manifest.json`（release-please 的版本账本）与
`plugins/forge-dsh/package.json`（随火车的 dsh 插件版本），保持逃生舱路径自洽——
`internal/ci/release_please_test.go` 的 `TestReleasePleaseManifest_MatchesNpmVersion`
守卫 `npm/package.json` == manifest、`TestReleasePleaseManifest_DshPluginTracksTrain`
守卫 dsh == manifest。只有绕过脚本手动打 tag 时才需要手动同步这些文件。

CI 暂坏需绕过 workflow 手动 `gh release` + `npm publish` 时，绕过的是 **整个 needs 链**
（沙盒验证无法覆盖手动行为）。此时：

1. **必须当场登记"CI 待修"待办**——v0.27.0 绕过 failure CI 当完成，是这次教训的根因
2. 绕过后第一时间修 CI，并补跑（重打 patch tag 走完整 release.yml 验证链路）
3. **绕过 ≠ CI 健康**：npm 包发出去了不代表发布链路 OK；CI 红着就是债

## 版本号规则

- 正常发版：Release PR 按 Conventional Commits 推断 patch/minor/major
- 发版后发现 bug：**升 patch 重发**，不 force-push 覆盖已发 tag
  - hazard-guard 会拦 force-push；且覆盖已发布 npm 包不可逆（registry 会缓存）
  - v0.27.1→v0.27.2 即此规则实例
