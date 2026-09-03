# golden 标注集（门禁 precision/recall 基线）

纪律（docs/design/forge-evaluation-system.md §二/§六）：用例期望与指纹 fail-closed；
干净样本被误触 = fpr = bug 级 finding；指纹不符拒绝运行（--rewrite-manifest 仅限
显式轮换）。

## v1 首轮实测结论（2026-09-03）

- **auto-compile 是提醒门禁，不是编译门禁**（v0.25 loop-engineering 降级：永不跑
  编译器，编译自检委托 agent）。v1 用例钉住其真实契约：活跃会话源码写入 → 输出
  "Advisory: 已修改源码"；research-mode 会话 → 抑制。
- 首轮曾按"编译错误应被拦截"构造用例，实测 missed——那是评测暴露的**主张错位**
  （设计叙述 vs 部署现实），不是用例错误。若产品要主张"编译错误被拦"，需要先在
  Forge 核心实现真正跑编译的检查（路线决策），评测无法替代。
- 2026-09-03 扩集：task-guard ×5（自保护拦截 .forge/* 与 .claude/settings*；
  protocol.yml 豁免；测试文件 TDD 豁免；非源码放行）+ file-sentinel ×5（真实
  bash-guard→写入→file-sentinel 配对：源码写隔离 / .cfg 配置漂移隔离 / 只读与
  forge 命令与已提交工作放行）——三门禁 12 例，precision 5/5、fpr 0/7、确定性 1.0。
- 下一步扩展：task-verify 的 test-coverage/work-activity 校验各补 5-10 例，
  向设计目标的 20-30 例/门禁爬坡。
