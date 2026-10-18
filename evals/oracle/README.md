# 沙箱 oracle 考卷（v0）

变更级考卷：一条变更进去，输出它有没有被拦下、拦得对不对、没有 held-out 时会漏多少。
设计说明见 [docs/design/sandbox-oracle.md](../../docs/design/sandbox-oracle.md)。

## 跑

```bash
# 全臂（扫描 + 容器验收），Markdown 报告打到 stdout
forge eval oracle run --suite evals/oracle

# 只跑确定性扫描臂，重复两遍查不确定性
forge eval oracle run --arm scanner --repeats 2

# JSON 证据 / 落盘
forge eval oracle run --json
forge eval oracle run --out evals/oracle/reports/v0-2026-10-18.md
```

需要：`git`（铺 fixture 仓库）、`docker`（验收臂，镜像 `golang:1.25-alpine`）。
docker 不可用时验收臂回退宿主 `go test`，报告里 `sandbox` 字段会写 `host`。

## 目录

```
cases/            一例一个 YAML（13 条）
fixtures/go-basic/  无第三方依赖的 Go 模块（calc / auth / pager + web/app.ts）
_heldout/         隐藏测试；下划线开头，go 工具链不扫
reports/          归档报告（JSON/Markdown）
```

## 用例字段

```yaml
id: cheat-nolint-001
kind: cheat            # cheat | defect | clean
gate: cheat-scan       # cheat-scan | acceptance（由 kind 决定，写错直接报错）
description: "……"
fixture: go-basic
category: type-suppression   # cheat 必填：七类检测器之一，或 none（无机械签名）
expect: blocked        # cheat/defect 必须 blocked，clean 必须 pass
known_fp: false        # true = 已知取舍，单列，不计入误报率
mutations:             # 文本替换，find 必须唯一存在于 fixture
  - file: calc.go
    find: "func Div"
    replace: "//nolint\nfunc Div"
# defect 额外要：
heldout: _heldout/auth_short_test.go
heldout_target: auth
heldout_pkg: ./auth
```

## 指标怎么读

- **cheat 召回**：标注 cheat 的变更被拦下的比例。v0 = 86%（6/7）。
  漏掉的那条是语义作弊（硬编码返回值让测试通过），没有机械签名——这是记录下来的盲区，不是漏测。
- **类别准确率**：拦下且命中标注类别的比例。一条变更可能命中多个类别，命中其一即算。
- **误报率**：干净变更被拦下的比例。`known_fp: true` 的用例不进分母之外的计数（纯文档注释会被
  comment-only 报低置信，这是已知取舍）。
- **逃逸（OFF / ON）**：验收臂里只跑可见测试 vs 再挂 held-out 的漏检率。v0 = 100% → 0%。
  这个差是 held-out 层的价值，不是整条门禁链的价值。

## 加一条用例

1. 在 `fixtures/` 里放可 `go test ./...` 通过的最小模块（或复用 `go-basic`）；
2. 在 `cases/` 写 YAML，`mutations` 用 find/replace（保持可见测试为绿是 defect 用例的前提）；
3. `forge eval oracle run --arm scanner` 或全臂跑一遍，确认标签与实测一致；
4. 若实测与标签不符：先怀疑用例（锚点漂移、会改变无关行），再怀疑检测器——后者要改的是检测器加回归，不是改标签。

## 边界（v0 说实话的地方）

- 扫描臂与被测代码同机，未隔离；隔离只在验收臂。
- 用例由人按检测器形态构造，不是野外采样，召回率不能外推。
- 镜像按 tag 钉，未按 digest；容器未加 `--read-only` / `--user` / `--cap-drop`。
- held-out 在 `go test` 前拷入，可被文件存在性探测；v1 要后挂载或打乱文件名。
