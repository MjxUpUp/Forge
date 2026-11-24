# 命令参考

> 这是 repo 内核心命令面的参考表。命令与 flag 以 \`forge <命令> --help\` 的实测输出为准；
> 代码与本文不一致时以二进制为准。cobra 自动生成的 \`completion\` 与 \`help\` 不单列。
>
> core-only 之后只保留三类命令：**契约与管道**（init/off/on/status/next/change/impact/test/compat/docs）、
> **任务与门禁**（task/review）、**版本**（version）。旧时代的宿主接线命令
> （harness/migrate/registry/project/node/workspace/worktree/skill/update/hook 等）已删除，
> 命令不存在就是不存在，不会再给出「已废弃」的兼容壳。

## 只跑两条门禁

任务的完整链路只有两条门禁命令，其余是记录：

1. \`forge task start\` —— 起任务，附 baseline（验收预跑）。\`--accept "run :: expected"\` 的 run
   按空白切 argv 直接 exec，不经 shell；expected 是子串匹配，空 expected 只看退出码。
   行为类改动要登记 \`--accept-red <命令>\`（开工时确实失败的判据），确实无可失败之物时用
   \`--red-first-waived "<理由>"\` 显式豁免，理由会进交付单。
2. \`forge task gate task-implement\` —— 核对确有改动；
   \`forge task verify-acceptance\` 实跑考卷并记确定性证据；
   \`git commit\` 后派只读评审 → \`forge review pass --reviewer <id> --criteria-reviewed <n>\`；
   \`forge task gate task-complete\` —— 硬查验收已实跑未过期、评审已记录、先失败证明已登记。

\`forge task complete\` 只做收尾记账，不放行任何新东西；\`forge task report --ref <ref>\` 出交付单。

## change 的五份产物

\`goal.md\`（Objective / Out of Scope / Check 三段）· \`change.md\`（改动/影响/验证）·
\`acceptance.md\`（\`\`\`bash 围栏判据）· \`clarify.md\`（边界与非目标 + 人确认，必须 \`--by-human\`）·
\`tasks.md\`（可选，跨多节点时才写）。阶段机只读文件：\`forge change phase <id>\` 会告诉你还缺哪个文件。
\`forge change verify <id>\` 实跑判据并封印证据，是唯一能产出「已验证」的命令；
证据绑定 commit 与文件 sha256，之后改文件即失效，重跑。

## 判据的硬约束

acceptance 围栏里不能用管道 \`|\`、\`||\`、后台 \`&\`、\`set +e\`、\`eval\`，也不能再套一层 shell
（\`bash -c\` / \`sh -c\` / \`\${IFS}\` 拼参数都算）——这些写法都能把失败变成 0。这是解析期拒绝，
不是沙箱；它认不出的写法（变量拼命令、脚本内部）不保证被拦，所以判据要短、直白、能被 review 看懂。

## 能力面自检

\`forge docs closure\` 用运行中的命令树核对活文档（README / AGENTS.md / docs/reference）里提到的
每个 \`forge …\` 命令是否真的存在，并统计注解覆盖；\`--strict\` 有漂移即非零退出。
\`forge change agents --check\` 对 AGENTS.md 托管块做同样的事（块由命令树生成，块头 \`forge:proto\`
记录正文 sha，内容不一致才报漂移，版本号不同只提示）。

## 命令与 flag 全表

下表由 \`forge compat snapshot\` 的命令面生成（\`forge <命令> --help\` 是同一真相源的交互形态）。

| 命令 | flag |
|------|------|
| \`forge change\` | — |
| \`forge change agents\` | --check, --pointers, --print |
| \`forge change check\` | --json, --staged |
| \`forge change clarify\` | --by-human, --force, --note, --note-file |
| \`forge change drift\` | --json |
| \`forge change gate\` | --task, --timeout |
| \`forge change init\` | --profile |
| \`forge change list\` | --json |
| \`forge change new\` | --by-human, --fast, --title |
| \`forge change phase\` | --json |
| \`forge change status\` | --json |
| \`forge change verify\` | --timeout |
| \`forge compat\` | — |
| \`forge compat report\` | --base, --json |
| \`forge compat snapshot\` | --out |
| \`forge docs\` | — |
| \`forge docs closure\` | — |
| \`forge docs lint\` | --base, --json |
| \`forge impact\` | --json |
| \`forge init\` | --agents, --fresh, --mode, --profile, --project |
| \`forge next\` | --json |
| \`forge off\` | --all, --commit |
| \`forge on\` | — |
| \`forge review\` | — |
| \`forge review gate\` | --ref |
| \`forge review pass\` | --criteria-reviewed, --findings, --note, --reason, --ref, --reviewer, --rotate-reviewer, --score |
| \`forge review status\` | --ref |
| \`forge status\` | --json, --system |
| \`forge task\` | — |
| \`forge task abort\` | --cascade, --detach-deps, --json, --ref |
| \`forge task accept\` | --assert, --ref |
| \`forge task answer\` | --content, --ref |
| \`forge task assign\` | --by, --ref, --role, --to |
| \`forge task attach\` | --ref, --session, --tool |
| \`forge task block\` | --content, --ref, --resolution, --resolve |
| \`forge task cancel\` | --reason, --ref |
| \`forge task checklist\` | — |
| \`forge task checklist add\` | — |
| \`forge task checklist drop\` | — |
| \`forge task checklist tick\` | — |
| \`forge task checkpoint\` | --label, --list |
| \`forge task claim\` | --as, --ref |
| \`forge task complete\` | --ref |
| \`forge task context\` | --json, --ref |
| \`forge task decide\` | --affects, --by, --content, --rationale, --ref, --rejected |
| \`forge task deliver\` | --as, --ref |
| \`forge task edgecheck\` | — |
| \`forge task export\` | --include-checklog, --output, --redact, --ref |
| \`forge task fail\` | --reason, --ref |
| \`forge task finding\` | --content, --evidence, --note, --ref, --reset-loop, --resolve, --source, --stats, --tag |
| \`forge task finish\` | --keep, --merge-to |
| \`forge task fuzz\` | --max, --sec |
| \`forge task gate\` | --ref, --silent |
| \`forge task health\` | --json |
| \`forge task heldout-pool\` | --add, --apply, --draw, --list, --name, --ref, --tag |
| \`forge task impact\` | --level, --note, --ref, --repo |
| \`forge task import\` | --file, --force, --merge |
| \`forge task intent\` | — |
| \`forge task list\` | --all-projects, --json, --open, --plan-conversion, --timeline, --workspace |
| \`forge task mine\` | --agent, --all-projects, --blocked, --json, --role |
| \`forge task mirror\` | — |
| \`forge task mirror github\` | --dry-run, --repo |
| \`forge task next\` | --ref |
| \`forge task override\` | --acceptance-gate, --ref, --skill-decisions, --work-activity |
| \`forge task question\` | --content, --ref |
| \`forge task reclaim\` | --dry-run, --json |
| \`forge task regression\` | --finding, --none, --note, --ref, --test |
| \`forge task reopen\` | --reason, --ref |
| \`forge task report\` | --json, --ref |
| \`forge task resume\` | --all-projects, --json, --match, --no-attach, --ref, --workspace |
| \`forge task rollback\` | — |
| \`forge task scope\` | — |
| \`forge task scope add\` | --ref |
| \`forge task scope show\` | — |
| \`forge task score\` | --history, --json, --ref |
| \`forge task start\` | --accept, --accept-file, --accept-red, --agent, --allow-empty-criteria, --allow-green, --assert, --assignee, --base, --branch, --change, --depends-on, --from-issue, --goal, --goal-file, --heldout, --invariant, --json, --kind, --origin-tool, --parent, --plan-file, --red-first-waived, --ref, --role, --scope, --tier, --tier-reason, --title, --ttl, --worktree, --wt-dir |
| \`forge task status\` | --json, --ref |
| \`forge task verify-acceptance\` | --ref, --trust-foreign |
| \`forge task watchdog\` | --release, --stall |
| \`forge task wild\` | — |
| \`forge test\` | — |
| \`forge test ledger\` | --approvals, --json, --max-age, --record, --strict |
| \`forge test probe\` | --json, --sample, --since |
| \`forge test record\` | --in, --out |
| \`forge version\` | — |
