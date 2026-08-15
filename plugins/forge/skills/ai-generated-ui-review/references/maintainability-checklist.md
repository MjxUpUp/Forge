# AI 生成 UI 审查清单（4 个独有块 + 通用维度指针）

本文件是 `ai-generated-ui-review` SKILL.md 步骤 2 加载的完整清单。通用维度（DRY/设计系统/a11y）不重复——各一行指针到 frontend-code-review / code-review-gate；本文件的正文是 4 个 AI 生成独有块 × 严重性分级，上线前必查。

## 目录

- [严重性定义](#严重性定义)
- [通用维度指针（不重复查）](#通用维度指针不重复查)
- [块 1：安全债（最高优先，生产化必查）](#块-1安全债最高优先生产化必查)
- [块 2：shadcn registry 供应链风险](#块-2shadcn-registry-供应链风险)
- [块 3：可维护性税量化（纵向指标）](#块-3可维护性税量化纵向指标)
- [块 4：来源风险（vibe coding 定级）](#块-4来源风险vibe-coding-定级)
- [判定矩阵](#判定矩阵)
- [审查心态（Addy Osmani 定调）](#审查心态addy-osmani-定调)

## 严重性定义

| 级别 | 含义 |
|---|---|
| **block** | 不可合并（安全/法律红线/结构性重写） |
| **fix** | 应当修复（可维护性税/规范偏离） |
| **suggest** | 建议考虑（优化） |

---

## 通用维度指针（不重复查）

| 维度 | 为什么对 AI 生成代码尤其重要 | 完整清单在哪 |
|---|---|---|
| **DRY 违反** | AI 生成头号问题：Lovable 官方文档自认 "do not reuse shared components unless clearly scoped"；[arXiv 2603.28592](https://arxiv.org/abs/2603.28592) 89.3% code smell 主因是重复 | code-review-gate 轨道 B「可维护性」；量化阈值用本文件块 3（重复率 >15% 警告 / >25% block） |
| **设计系统脱节** | AI 生成倾向硬编码色值/间距而非走 token、自造组件而非复用项目组件 | frontend-code-review 维度 4「Design Token 一致性」；修 token 抽取用 design-system-workflow |
| **a11y 缺失** | AI 生成 UI 普遍缺 a11y（`<div onClick>`、无 reduced-motion、焦点未管理） | frontend-code-review 维度 1「a11y」 |

---

## 块 1：安全债（最高优先，生产化必查）

**依据**：[Georgia Tech Vibe Security Radar](https://news.research.gatech.edu/2026/04/13/bad-vibes-ai-generated-code-vulnerable-researchers-warn) CVE 累计 74 个（2026-03 单月 35 个）；[OX.Security](https://www.ox.security/blog/vibe-coding-security) 62% 有漏洞；[Escape.tech](https://escape.tech/blog/methodology-how-we-discovered-vulnerabilities-apps-built-with-vibe-coding/) 1400 应用 2000+ 严重漏洞。

### Block（命中任一不可合并）
- **API key / 密钥前端暴露**：
  - 搜 `process.env.NEXT_PUBLIC_*` 误用（凡 NEXT_PUBLIC_ 进 client bundle）
  - 硬编码 key（`sk-xxx`/`AIza`/`ghp_`）
  - `.env` 进 client 构建
- **数据库无认证**：client 直连 DB、API 无 auth 中间件
- **SQL 拼接**：未参数化查询、未用 ORM
- **BOLA（越权）**：用户能访问他人资源（[Lovable 曾 BOLA 暴露 48 天](https://thenextweb.com/news/lovable-vibe-coding-security-crisis-exposed)）
- **无输入校验**：未用 zod/valibot 校验请求体
- **CORS 全开 + 凭证**：`Access-Control-Allow-Origin: *` 配合 `credentials: include`
- **命令注入**：未 sanitizing 的输入拼进 exec/spawn

### Fix
- **secrets 在日志**：console.log(req.body) 泄露密码/token
- **弱加密/哈希**：MD5 存密码、自造加密

### 检测命令
```bash
# 搜潜在 key 泄露
grep -rE '(api[_-]?key|secret|token|password|sk-|AIza|ghp_)' --include='*.ts' --include='*.tsx' src/
# 搜 NEXT_PUBLIC_ 误用
grep -r 'NEXT_PUBLIC_' src/ | grep -v '\.d\.ts'
```

---

## 块 2：shadcn registry 供应链风险

### Block
- **非可信 registry 来源**：RCE 注入攻击面（DEV.to "Risk of Registry Injection Attacks with shadcn"）
- **未审查的 `npx shadcn add <url>`**：执行前未审计 registry.json 内容
- **registry 组件含 postinstall 脚本**：潜在恶意执行

### 审查步骤
1. 确认 registry 来源（官方/可信第三方/未知）
2. 审计 registry.json 的 `dependencies` + `files`
3. 检查是否有 postinstall/preinstall 脚本
4. 企业项目：只用自建 registry

---

## 块 3：可维护性税量化（纵向指标）

### 量化检测
- **重复率**：jscpd / duplicate-code-detection
  - >15% 警告，>25% block
- **圈复杂度**：eslint complexity / plato
  - 单组件 >10 警告，>20 block
- **包大小**：bundle analyzer
  - Framer Motion 125KB 仅用 fade-in → suggest 换 CSS
  - Spline runtime 544KB 不可 tree-shake → 评估必要性
  - moment.js → 换 dayjs/date-fns
- **存活技术债**：[arXiv 2603.28592](https://arxiv.org/abs/2603.28592) 22.7% AI 技术债存活
  - 标记的 TODO/FIXME 必须修，不能"先留着"

### 检测命令
```bash
# 重复代码
npx jscpd src/ --threshold 15
# 圈复杂度
npx complexity-report src/ --maxcc 10
# bundle 分析
ANALYZE=true npm run build
```

---

## 块 4：来源风险（vibe coding 定级）

- **无 spec.md = vibe coding**：风险翻倍——所有发现升一级看待，判定矩阵按"无 spec"行执行
- **生成工具可确认时按已知问题模式加权**（v0/Bolt/Lovable/Replit/Cursor 各有高发模式）；确认不到不阻断审查，只是失去加权信号
- **通过功能测试 ≠ 适合生产**（[arXiv 2508.14727](https://arxiv.org/abs/2508.14727)）：必跑 SAST + 本清单逐项查
- **把 AI 产出当"成品"而非"起点"** 本身即是 Red Flag

---

## 判定矩阵

| 条件 | 判定 |
|---|---|
| 无 spec + 多 block | ❌ 重写（走 ai-ui-generation-workflow 重新生成） |
| 有 spec + block 全修 | ✅ 可合并 |
| 有 spec + fix 多 | ⚠️ 需改造后合并（走 ai-ui-generation-workflow 阶段 3 生产化 5 步） |
| 重复率 >25% | ❌ 重构后再审 |
| 任一安全 block | ❌ 不可合并，立即修 |

---

## 审查心态（Addy Osmani 定调）

> "treat every AI-generated snippet as if it came from a junior developer."

- 通过功能测试 ≠ 适合生产（[arXiv 2508.14727](https://arxiv.org/abs/2508.14727)）
- AI 生成 = 起点，不是终点
- 不审查 = 制造永久技术债（22.7% 存活率）
