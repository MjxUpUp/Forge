# W1 Spec：hazard-guard 语义分词层（GuardFall 五类绕过补漏）

## 问题（当前断点）

Adversa AI GuardFall（2026-06-30）实测 11 个开源 agent 的命令 guard，10 个被五类
shell 混淆技巧全部绕过（「Lexing is not evaluation」）。Forge 的 hazard-guard
is_hazardous 在原始命令串上做 case-glob/grep 匹配，与阵亡者同构——五类绕过
在原始串上词法不可见：

| 类 | 形态 | 原始层结果 |
|---|---|---|
| A 引号并词 | `r"m" -rf` / `r''m` / `r\m` | 漏放 |
| B 变量分词 | `rm$IFS-rf` / `rm${IFS}-rf` | 漏放 |
| C 命令替换 | `$(echo rm) -rf` / 反引号内层 | 漏放 |
| D 管道进解释器 | `… \| base64 -d \| sh`、`curl … \| bash` | 漏放 |
| E 替代 argv | `find / -delete`、`dd of=/dev/*` | 漏放 |

## 方案（canonicalize-then-check）

在 hazard-guard 脚本内加**语义分词层**（纯 bash/awk，零新增进程/零新增 hook
条目——响应「hook 太多太重」痛点，本层只在 raw-miss 路径多花一次 awk）：

1. `hazard_tokens`（awk）：bash 词法规范化——引号/反斜杠字符剥离、内容保留
   （词合并）；未引用 `$IFS`/`${IFS}` 视为分词符；未引用 `; | &` 与换行切段；
   `$()`/反引号内层提取（marker 标注 + 递归扫描，深度 ≤2）；heredop 体跳过
   （落盘数据，与 strip_quotes 同款 opener 规则，歧义 broken → 语义层让位原始层）。
2. `hazard_semantic_eval`：按段评词——命令位（跳过赋值前缀）命中危险命令族
   （rm/xargs/sudo 的 rf 簇 + 临时区白名单复用、git push/reset、kubectl delete、
   docker prune/volume rm/rm -f、find -delete、dd of=/dev/*）；命令位不可解析
   （替换 marker / 含 `$` 变量）→ fail-closed；管道终点是 shell/SQL 解释器 → HITL。
3. 门禁接线：raw 层未命中才跑语义层；语义命中跳过数据上下文放行路径，直入
   confirm 链；block 输出带「拦截原因（语义层）」行。

**不变量**：原始层、rm 白名单（safe_mktemp_vars/is_tmp_rm_target）、confirm 链、
FORGE_ALLOW_HAZARD 移除语义全部不动。语义层只补漏，不改既有判定。

## 已知边界（诚实披露）

- ANSI-C 引用 `$'\x20'`、sq 内 `$IFS`：不可判定层，confirm 链是真门禁。
- sudo flag 带值形态（`sudo -u user cmd`）：user 会被误当命令词放行——原始层
  对其中含 rm 的形态仍有子串兜底。
- git 全局选项只覆盖 `-C`/`-c a=b` 常见形态的子命令扫描，非全量枚举。
- SQL 解释器管道规则对只读查询（`echo "SELECT …" | sqlite3 db`）同样 HITL
  （方向安全，confirm 可豁免）；反引号内层以 `$( ` 开头的形态 fail-closed。
- bash-guard 的 write 检测不在本层（file-sentinel 事后对账是其后盾）。

## 审查回应（2026-09-12 只读审查 NEEDS-FIX → 已修复）

- C1 段模型不认换行（多行命令良性首行洗白 A/B/C）：awk 行界补发 S；引号跨行
  与反斜杠续行不切段；heredoc 分隔行闭合后补 S；多行测试与 golden 补齐。
- M1 sudo 前缀击穿 find/dd 家族：sudo 解包（尾部首个非 flag 词重评）。
- M2 `&&`/`;` 链误拦「管道终点」且原因失实：分隔符分级 P/S，管道规则只对
  管道右段生效；良性对照（cd && bash / make; sh）钉死。
- M3 逃生激活时证据束谎称无未测区域 + 重复审计行 + 双算 git：配对逻辑抽出
  单一源 coveragePairing；披露走无视逃生的 CoveragePairingForDisclosure；
  复用 BuildEvaluateInput 同趟结果（CoverageMissing 穿透），零新增审计行、
  零额外 git 枚举（逃生分支除外）。
- minor：超限汇总行只数 open；rm 长选项死分支删除（字母簇本就覆盖 --force）；
  git 子命令全词扫描覆盖 -C/-c 形态；语义层用例断言原因行。

## 验收（可测判据）

- 五类绕过 + 多行/sudo/git 选项扩展形态 golden 全拦截（evals/forge/golden/
  hazardguard-blocks-guardfall-*.yaml，6 条；sudo/git 选项扩展形态由脚本级
  测试覆盖）+ 脚本级测试
  TestHazardGuardScript_GuardFall*（6 函数；block 用例带「拦截原因（语义层）」
  断言）。
- 既有行为契约零回归：hazard_script_test 全部（mktemp 白名单/TMPDIR/多行数据
  上下文/git branch -d/confirm 链文案）+ hazardguard 既有 2 条 golden。
- 披露面：gates-card v2 增 checker_attestation 节（GuardFall A-E 逐行：绕过类→
  检测层→可执行证据），缺节 fail-closed。
- 重量不回潮：hook 接线规模冻结棘轮（TestHookWiring_FreezeRatchet：8 事件/
  11 matcher/33 条目）+ 同 matcher 内重复命令检测。
