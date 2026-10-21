# Forge 演示：识破"假绿"

这份文档给第一次接触 Forge 的人照着跑。三个小演示说明一件事：测试显示全绿，不代表改动真的被验证过；Forge 会把这类"假绿"标出来。全程在终端里操作，不需要 AI agent。

每段预期输出都带一个标签：**【已实跑】** 表示 2026-10-21 在隔离的 HOME 下照本文命令跑过，输出逐字相同；**【源码原文】** 表示没有实跑，但文案已和 Forge 源码逐字核对；**【推算】** 表示没有实跑、按源码推断，以你实跑的结果为准。演示一、演示二的 2a 已实跑；2b、演示三和"回传证据"未实跑。文案出处集中在文末"核对记录"。

## 前置
- Go 工具链（演示一、二要跑 `go test`）、git 2.28 及以上（需要支持 `git init -b`）。
- Forge ≥ 1.77.0：演示一用到的"基线先绿"检查从这个版本开始有。装完用 `forge --version` 确认。

```bash
npm install -g @agent_forge/forge
```

两个术语：**checklog** 是 Forge 在本机记的检查流水账，每次检查落一行，事后可导出；**advisory** 指只提示、不阻断的检查——命令照常成功退出，结果记进 checklog。

## 演示一：预先置红的包测试，不会再被当成"全部杀灭"
mutation 抽样会把你改过的 Go 代码里的比较和逻辑算子翻转（例如 `>` 翻成 `<=`），然后跑测试，看测试能不能抓住。问题在于：如果测试本来就是红的，每个变异体都会"被杀灭"，杀灭率显示 100%，这就是假绿。现在 Forge 会先在不变异的状态下跑一次测试，基线是红的就不计分。

1. 建一个示例仓库，先提交一个基线绿的版本：

```bash
mkdir calc-demo && cd calc-demo && git init -q -b main && go mod init example.com/calc
```

```bash
printf 'package calc\n\nfunc Max(a, b int) int {\n\tif a > b {\n\t\treturn a\n\t}\n\treturn b\n}\n' > calc.go
```

```bash
printf 'package calc\n\nimport "testing"\n\nfunc TestMax(t *testing.T) {\n\tif Max(2, 1) != 2 || Max(1, 2) != 2 {\n\t\tt.Fatal("Max wrong")\n\t}\n}\n' > calc_test.go
```

```bash
git add -A && git commit -qm init
```

2. 接入 Forge 并开一个任务（`--branch` 会从 main 切出名为 ref 的新分支），然后在任务里改一处源码，同时把包测试改成恒失败：

```bash
forge init
```

```bash
forge task start --ref feat/mutation-demo --branch --accept "go test ./..."
```

```bash
printf '\nfunc Clamp(x, lo, hi int) int {\n\tif x < lo {\n\t\treturn lo\n\t}\n\tif x > hi {\n\t\treturn hi\n\t}\n\treturn x\n}\n' >> calc.go
```

```bash
printf '\nfunc TestPreRed(t *testing.T) { t.Fatal("预先置红") }\n' >> calc_test.go
```

3. 跑 mutation 抽样：

```bash
forge task mutation
```

预期输出【已实跑】（第一行走 stderr，其余走 stdout）：
```
  ⚠ 基线红：. 未变异时测试就未通过——跳过其变异体（杀灭率会被既有失败伪造）
⚠ 基线红：. 未变异时测试就未通过——这些包的 3 个变异体未执行，下方杀灭率只覆盖基线绿的包
mutation 抽样结果：样本 0（killed 0 / survived 0 / invalid 0），杀灭率（仅基线绿的包）n/a
→ 先修好基线红的包测试再重跑本命令；证据已落 checklog（advisory 不阻断 complete）
```
作为对照，1.77.0 之前同样的操作会直接打印 `✅ 全部杀灭——考卷对注入 bad case 的防御有效`。这句话现在仍在，只有基线绿而且没有存活变异体时才会出现。

判定结果只写进 checklog，不打印在屏幕上，用导出命令看：

```bash
forge task export --include-checklog | grep '"verdict"'
```

预期【已实跑】：`"verdict": "baseline-red"`。用 `forge trace feat/mutation-demo` 也能看到这一行【源码原文】：`mutation-sampling` 前面标 ⚠，详情以"mutation 抽样：1 个包未变异时测试就不是绿的"开头。

4. 把测试恢复成提交时的版本，再跑一次，看真实的杀灭率：

```bash
git checkout -- calc_test.go
```

```bash
forge task mutation
```

预期输出【已实跑】（前两行 ☠ 走 stderr，其余走 stdout）：`Clamp` 没有测试，它的两个变异体存活。
```
  ☠ 存活 calc.go:11（<→>=）——测试没抓住这道 bad case
  ☠ 存活 calc.go:14（>→<=）——测试没抓住这道 bad case
mutation 抽样结果：样本 3（killed 1 / survived 2 / invalid 0），杀灭率 33%
  ☠ 存活 calc.go:11（<→>=）
  ☠ 存活 calc.go:14（>→<=）
存活 = 注入的 bad case 测试没抓住——按位点补断言（修测试，不是删变异）
→ 按存活位点补断言后重跑本命令；证据已落 checklog（advisory 不阻断 complete）
```

## 演示二：识破吞错误和永假分支
Forge 有一个 cheat-scan 检查，扫描任务新增的代码行，按机械规则匹配 7 类常见的"作弊"写法：`type-suppression` / `error-swallow` / `dead-branch` / `comment-only-fix` / `comment-as-debt` / `phantom-import` / `path-assumption`。它是 advisory 检查。cheat-scan 不扫测试文件，所以"删断言、削弱测试"不在它的检测范围内；那一类由 agent 编辑文件时的断言检查负责，纯终端演示不出来。

**2a. 一条命令自检（推荐，不改你的仓库）。** 在演示一的仓库里运行：

```bash
forge eval redteam
```

它在临时目录里埋五颗雷，再用 Forge 已有的检查逐颗判定能不能拦住。与本节相关的是 swallowed-error 和 type-suppression 两行，其余三行是 Forge 的其他检查。预期输出【已实跑】：
```
redteam 演练（seeded-bug 红队）：拦截率 5/5
  ✅ intercepted zero-acceptance-delivery     ← acceptance registration gate（考卷缺位硬拦）
  ✅ intercepted unused-internal-export       ← unused-gate（internal/ 零引用导出硬拦）
  ✅ intercepted swallowed-error              ← cheat-scan error-swallow（新增行吞错模式）
  ✅ intercepted type-suppression             ← cheat-scan type-suppression（新增行类型抑制）
  ✅ intercepted heldout-gap                  ← held-out 双套件 gap（可见绿而保留集挂）
```
有雷没拦住时会标 `❌ escaped`，命令以非零退出码结束。

**2b. 在自己的任务里触发（未实跑）。** 接着演示一的任务，新建一个源文件，故意吞掉错误并写一个永假分支，然后依次推进两道门禁（门禁是 Forge 任务的阶段检查点：实现、验证、完成）：

```bash
printf 'package calc\n\nimport "strconv"\n\nfunc Parse(s string) int {\n\tn, err := strconv.Atoi(s)\n\t_ = err\n\tif false {\n\t\treturn -1\n\t}\n\treturn n\n}\n' > parse.go
```

```bash
forge task gate task-implement
```

```bash
forge task gate task-verify
```

预期 stderr【源码原文的格式；行号为推算】：
```
ADVISORY: [task-verify] cheat-scan 命中 2 处疑似 AI 作弊模式（advisory 不阻塞；机械检测供 review 核查）
  ⚠ [high|error-swallow] parse.go:7 — _ = err
  ⚠ [high|dead-branch] parse.go:8 — if false {
```
parse.go 没有配对测试，所以同时还会出现一行测试配对的 advisory，可以忽略。如果 task-verify 被其他门禁拦下（例如工作活动检查），以 2a 的结果为准，并把完整输出贴到 [GitHub Issues](https://github.com/MjxUpUp/Forge/issues)。

## 演示三（可选，未实跑）：首证据路径
"首证据"指在你的仓库里第一次拿到一条由真实测试运行产生的验收记录。先跑一次 60 秒自检。它在隔离的临时目录里走完 8 步：init → 带一条刻意先红的验收标准开任务 → 验收如实失败 → 修复 → 验收通过 → trace 看到证据。

```bash
forge eval wedge-drill
```

通过时末尾打印【源码原文】`WEDGE-DRILL PASS — 8 steps, total <耗时>`，失败时打印 `WEDGE-DRILL FAIL at "<步骤名>"`。

然后在你自己的仓库里走同一条路径（把验收命令换成你项目的测试命令；`--branch` 要求仓库里有 main 或 master 分支，没有的话去掉它，留在当前分支上做）：

```bash
forge init
```

```bash
forge task start --ref demo/first-evidence --branch --accept "go test ./..."
```

```bash
forge task verify-acceptance
```

结论行【源码原文】是 `✅ 全部通过 — 真实结果已记为 deterministic 证据（checklog: acceptance）`，或者 `❌ 存在未通过项 — 失败结果已记入 checklog（acceptance）`。

## 回传证据（未实跑）

```bash
forge task export --ref demo/first-evidence --include-checklog --redact -o forge-evidence.json
```

- 用 `forge task export --redact`，不要用 `forge project export`：后者没有 `--redact`，它自己的帮助文本也指向前者。
- `--redact` 会抹掉 Goal/Summary/分支/commit、验收命令原文，以及 checklog 的 Detail/SessionID，但保留检查名、是否通过、级别和来源，所以我们仍能看到"跑过哪些检查、是否通过"。
- checklog 行的 `meta` 字段不会被脱敏，例如 mutation 行的 `baseline_red` 里有包路径。发送前请先打开文件看一遍。

## 核对记录（文案出处）

| 文案 | 出处 |
|---|---|
| mutation 基线红的 stderr 行 | `internal/taskpipeline/mutation.go:RunMutationSampling` |
| mutation 结果摘要、存活列表、"存活 = …"行 | `internal/taskpipeline/mutation.go:FormatMutationSummary` |
| "→ 先修好基线红…" / "→ 按存活位点补断言…" / "✅ 全部杀灭…" | `internal/clitask/task_questioning.go:mutationNextStep` |
| `"verdict": "baseline-red"` | `internal/taskpipeline/mutation.go:mutationVerdict` |
| 1.77.0 之前的直接打印 | `ce8424bc^` 的 `internal/clitask/task_questioning.go:runTaskMutation` |
| cheat-scan 7 类、跳过测试文件 | `internal/taskpipeline/cheatscan.go:ScanCheatPatterns` / `isTestFile` |
| redteam 输出格式 | `internal/taskpipeline/redteam.go:FormatRedteamReport` |
| cheat-scan ADVISORY 行格式 | `internal/taskpipeline/executor_check_verify_scans.go:scanCheatFindings` |
| wedge-drill 8 步与 PASS/FAIL 行 | `internal/evalkit/wedgedrill.go:wedgeScript`、`internal/cli/eval.go:runEvalWedgeDrill` |
| verify-acceptance 结论行 | `internal/clitask/task_gate.go` |
| `project export` 帮助文本 | `internal/cli/project_export.go` |
| `--redact` 不处理 meta | `internal/clitask/task_port.go:redactChecklogEntries` |
