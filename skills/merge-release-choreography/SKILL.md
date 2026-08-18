---
name: merge-release-choreography
description: "合并发版收尾编排：预检→卫生清扫→commit→merge→tag/构建→push→registry 核查→清分支→装机验证。Use when: 用户说'准备合并''合并发版''提交合并推送''准备push''发版清理分支''准备发版''ok准备合并发版'等收尾指令时、一个功能分支要走完合并到发布的最后一段时、发版后要验证安装是否生效时。SKIP: 发布前 go/no-go 门禁清单（用 release-readiness，本 skill 消费其结论不重审）、单次 diff 代码审查（用 code-review-gate）、运行时 bug 排查（用 systematic-debugging）、只写代码不发布（用 implementation-discipline）。"
metadata:
  pattern: pipeline
  domain: operations
  steps: 8
  composes: [release-readiness, code-review-gate, test-discipline]
  triggers: [{"event":"UserPromptSubmit","keywords":["准备合并","合并发版","提交合并","合并推送","发版清理","清理分支","准备push","准备发布","准备发版","合并并发布","merge and release"],"cooldown":300}]
---

# 合并发版收尾编排

从"代码写完了"到"用户装上了"之间的最后一段流水线。这段路每一步都踩过坑：tag 打错、镜像滞后误判、游离 exe 抢路、commit 顺序颠倒被 quarantine——全部来自真实事故。用户说"准备合并发版清理分支"时，按本编排逐站推进，不要现场重新推导。

## 核心原则

- **顺序即安全**：各站有依赖（未过预检不得 commit，未 commit 不得 complete，未发版不得验证）。跳站 = 把后面的坑提前引爆。
- **编排不重审**：质量判断委托给专门 skill（release-readiness 管 go/no-go、code-review-gate 管 diff），本 skill 只管顺序、卫生和收尾，不重复其清单。
- **发版不可逆**：tag 推出、包发布后回滚成本 >> 发前查 30 分钟。每站过完再按下一站。

## 流程（8 站）

### S1. 预检（diamond gate：不过则停）

- 测试全绿（真实跑测试命令，不是"上次跑过"）。
- **契约变更型重构后，e2e/回归场景的期望本身要同步审计**：改变资产布局/行为契约的重构，单元测试全绿 ≠ e2e 在断言新契约——场景可能还在钉旧行为而全红（Forge 自身事故：user-level-assets 重构后 Nightly 4 场景仍断言重构前的项目级布局，全红一轮才被发现）。重构收尾时把「e2e 期望审计」列入完成标准。
- review 快照绑定：若有 forge 任务，确认 review pass 绑定当前 HEAD（审查后改码须重跑，否则 task-complete 拒绝）。
- release-readiness 结论：已有 GO / GO-WITH-RISK 结论才继续；没有 → 先跑它，本 skill 不替代。
- **未过 → 停在修复，不进 S2。**

### S2. 卫生清扫（提交前把仓库收拾干净）

逐项扫描并清理，这些是"代码能跑但不该入库"的残留：

- 调试残留：console.log / fmt.Println 调试输出、临时代码、注释掉的大块死代码。
- 生成物与临时文件：build 产物、.tmp、临时脚本；确认 .gitignore 覆盖。
- **借鉴痕迹清理**（本站专项）：移除代码/注释/文档中对参考项目、第三方来源的描述性引用；**commit message 同样不得携带**借鉴来源描述——历史事故：发版后才发现 README 和 commit 里带着参考项目名。
- TODO 治理：本次范围内的 TODO 要么做掉要么转 issue，不留"以后再说"。

### S3. Commit

- commit 信息描述**变更内容和原因**（不是 diff 复述）。
- forge 任务顺序铁律：**commit 必须在 task complete 之前**（complete 清空 active ref，之后提交源码会被 file-sentinel quarantine）。正确顺序：三门禁 → commit → complete。
- 提交前 test-discipline 已在 PreToolUse 挡过断言弱化，这里不重审。

### S4. Merge

- 确认目标分支（通常 main/master）；fetch 最新远端，避免基于过期 base 合并。
- squash 还是保留分支提交：跟随项目惯例（看 git log 最近合并的形态），不确定就问。
- merge 冲突 → 停下逐文件解决并重跑测试，禁止"以我这边为准"批量覆盖。

### S5. Tag / 构建 / Release

- 版本号一致性（源码常量 / 包文件 / tag）——细节归 release-readiness M1，此处只确认已过。
- 打 tag 前确认 tag 不存在（已 push 的 tag **绝不可复用**）。
- 构建产物烟测：`<binary> --version` 报告新版本号。
- 签名工具版本敏感（cosign v3 用 --bundle，不是已废弃的 --output-signature）。

### S6. Push + Registry 核查

- push 主分支 + tag。
- **registry 滞后防误判**：`npm view` 走镜像源（如华为云镜像）会滞后显示旧版——查官方 registry API（curl 官方源）确认发布成功，不要凭镜像断言失败而重发。
- Release notes / changelog 与版本对齐。

### S7. 清理分支

- 删除已合并的 feature 分支（本地 + 远端）。
- 确认无其他分支引用该 HEAD 再删；删错了 reflog 也能捞，但别依赖。

### S8. 装机验证（发版的最后一公里）

- 用**真实安装路径**验证（npm install / 安装脚本），不是本地 build 直接跑。
- 验证版本：注意 PATHEXT 游离 exe 坑——npm 装新版但 `forge` 命中旧版手动 exe；**诊断必须 `cmd /c <cmd>` 走 Windows 解析链，bash 下查不出**。
- 验证核心命令一条（--version / status），确认新行为生效。

## 决策树

- 用户指令是完整仪式（"合并发版清理分支"）→ S1→S8 全走。
- 用户只要发版不合并（已在主分支）→ 跳 S4/S7，其余照走。
- 预检发现测试红 / review 未过 → 停在 S1 报告阻断项，不"先合了再说"。
- S6 registry 查不到新版本 → 先 curl 官方源区分"真没发出去"vs"镜像滞后"，前者修复重发，后者等待即可。

## 执行后自查清单

- [ ] 测试绿 + review pass 快照绑定当前 HEAD
- [ ] 卫生清扫完成（含借鉴痕迹，代码 + commit message 两层）
- [ ] commit 在 task complete 之前完成
- [ ] tag 唯一且未复用；产物 --version 正确
- [ ] registry 用官方源核实
- [ ] 装机验证走真实安装路径且 cmd /c 诊断
- [ ] 分支已清理，无残留

## Gotchas（真实事故库）

| 坑 | 现象 | 解决 |
|---|---|---|
| commit 在 complete 之后 | 源码被 file-sentinel quarantine | 顺序铁律：门禁→commit→complete；已发生则开 chore/*-commit 任务放行 |
| 镜像滞后误判 | npm view 显示旧版，误以为发布失败 | curl 官方 registry API 核实 |
| PATHEXT 游离 exe | 装了新版但命令跑的是旧版 | cmd /c 诊断（bash 查不出），清掉 npm-global 根下的手动 exe |
| tag 复用 | 强推已发布 tag，用户侧缓存混乱 | tag 推出即不可变，错了发新版本 |
| cosign 参数废弃 | 签名产物为空、发版失败 | 锁定 cosign v3 语法（--bundle）并 pin 版本 |

## 与其他 skill 的分工

- **release-readiness**：S1 消费其 GO 结论，本 skill 不重审 go/no-go 清单。
- **code-review-gate**：S1 前置的 diff 审查归它。
- **test-discipline**：测试质量与断言守卫归它，S1 只看绿/红。
- **implementation-discipline**：编码期纪律，S2 卫生清扫是它"聚焦变更"原则的发布期镜像。
- **systematic-debugging**：任一站失败需要找根因时转它。
