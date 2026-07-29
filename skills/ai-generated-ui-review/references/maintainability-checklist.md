# AI 生成 UI 可维护性评估清单（6 类）

本文件是 `ai-generated-ui-review` SKILL.md 步骤 2 加载的完整清单。6 类 × 严重性分级，上线前必查。

## 目录

- [严重性定义](#严重性定义)
- [类 1：DRY 违反（AI 生成的头号问题）](#类-1dry-违反ai-生成的头号问题)
- [类 2：安全债（最高优先，生产化必查）](#类-2安全债最高优先生产化必查)
- [类 3：设计系统脱节](#类-3设计系统脱节)
- [类 4：a11y 缺失（AI 生成普遍缺）](#类-4a11y-缺失ai-生成普遍缺)
- [类 5：shadcn registry 供应链风险](#类-5shadcn-registry-供应链风险)
- [类 6：可维护性税量化（纵向指标）](#类-6可维护性税量化纵向指标)
- [判定矩阵](#判定矩阵)
- [审查心态（Addy Osmani 定调）](#审查心态addy-osmani-定调)

## 严重性定义

| 级别 | 含义 |
|---|---|
| **block** | 不可合并（安全/法律红线/结构性重写） |
| **fix** | 应当修复（可维护性税/规范偏离） |
| **suggest** | 建议考虑（优化） |

---

## 类 1：DRY 违反（AI 生成的头号问题）

**依据**：Lovable 官方文档自认 "do not reuse shared components unless clearly scoped"；arXiv 2603.28592：89.3% code smell 主因。

### Block
- **3+ 近似组件各自实现**：ButtonA/ButtonB/ButtonC、UserCard1/UserCard2 各写各的 → 必须抽公共组件
- **整段逻辑 copy-paste ≥3 处**：未抽 hook/工具函数

### Fix
- **inline 样式/逻辑重复**：相同样式块或逻辑块出现 2 次以上
- **同一图标/常量硬编码多处**：未集中管理
- **props 传递链 >3 层**：prop drilling 该用 Context/store

### 量化指标
- 重复率 > 15% → 警告（用 jscpd/duplicate-code-detection 检测）
- 重复率 > 25% → block

---

## 类 2：安全债（最高优先，生产化必查）

**依据**：Georgia Tech Vibe Security Radar CVE 6→74；OX.Security 62% 有漏洞；Escape.tech 1400 应用 2000+ 严重漏洞。

### Block（命中任一不可合并）
- **API key / 密钥前端暴露**：
  - 搜 `process.env.NEXT_PUBLIC_*` 误用（凡 NEXT_PUBLIC_ 进 client bundle）
  - 硬编码 key（`sk-xxx`/`AIza`/`ghp_`）
  - `.env` 进 client 构建
- **数据库无认证**：client 直连 DB、API 无 auth 中间件
- **SQL 拼接**：未参数化查询、未用 ORM
- **BOLA（越权）**：用户能访问他人资源（Lovable 曾 BOLA 暴露 48 天）
- **无输入校验**：未用 zod/valibot 校验请求体
- **CORS 全开 + 凭证**：`Access-Control-Allow-Origin: *` 配合 `credentials: include`
- **命令注入**：未 sanitizing 的输入拼进 exec/spawn

### Fix
- ** secrets 在日志**：console.log(req.body) 泄露密码/token
- **弱加密/哈希**：MD5 存密码、自造加密

### 检测命令
```bash
# 搜潜在 key 泄露
grep -rE '(api[_-]?key|secret|token|password|sk-|AIza|ghp_)' --include='*.ts' --include='*.tsx' src/
# 搜 NEXT_PUBLIC_ 误用
grep -r 'NEXT_PUBLIC_' src/ | grep -v '\.d\.ts'
```

---

## 类 3：设计系统脱节

AI 生成倾向硬编码而非走 token。

### Fix
- **硬编码色值**：`#5e6ad2` → `var(--color-brand-primary)`
- **硬编码间距/圆角/阴影**：未走 design token
- **未复用项目组件**：自己造 Button 而非用 `@/components/ui/button`
- **字体/圆角/阴影风格不一致**：与项目 design language 偏离

### Suggest
- **图标库混用**：同时引入 lucide/heroicons/feather 多套

---

## 类 4：a11y 缺失（AI 生成普遍缺）

### Block
- **`<div onClick>` 处理交互**：无键盘可达性
- **动效无 reduced-motion**：违反 WCAG 2.2 SC 2.3.3
- **modal 焦点未管理**：打开未移入、关闭未归还、未 trap focus

### Fix
- **缺 ARIA/键盘导航**
- **对比度不足**
- **表单无 label**

→ 详细 a11y 清单用 `frontend-code-review` 维度 1。

---

## 类 5：shadcn registry 供应链风险

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

## 类 6：可维护性税量化（纵向指标）

### 量化检测
- **重复率**：jscpd / duplicate-code-detection
  - >15% 警告，>25% block
- **圈复杂度**：eslint complexity / plato
  - 单组件 >10 警告，>20 block
- **包大小**：bundle analyzer
  - Framer Motion 125KB 仅用 fade-in → suggest 换 CSS
  - Spline runtime 544KB 不可 tree-shake → 评估必要性
  - moment.js → 换 dayjs/date-fns
- **存活技术债**：arXiv 22.7% AI 技术债存活
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

## 判定矩阵

| 条件 | 判定 |
|---|---|
| 无 spec + 多 block | ❌ 重写（走 ai-ui-generation-workflow 重新生成） |
| 有 spec + block 全修 | ✅ 可合并 |
| 有 spec + fix 多 | ⚠️ 需改造后合并（走 ai-ui-generation-workflow 阶段 3 生产化 5 步） |
| DRY 违反 >25% | ❌ 重构后再审 |
| 任一安全 block | ❌ 不可合并，立即修 |

---

## 审查心态（Addy Osmani 定调）

> "treat every AI-generated snippet as if it came from a junior developer."

- 通过功能测试 ≠ 适合生产（arXiv 2508.14727）
- AI 生成 = 起点，不是终点
- 不审查 = 制造永久技术债（22.7% 存活率）
