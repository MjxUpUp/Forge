---
name: secure-coding
description: "安全编码强制规范：OWASP Top 10 (2021) + Threat Modeling (STRIDE) + Secret Management + 输入验证 + 输出编码 + 依赖审计 / SBOM。Use when: 写新代码、加鉴权/API、audit 安全漏洞、评估第三方库、处理用户输入/敏感数据、写 ADR 关于安全选型时。SKIP: 网络/WAF 部署（用 release-readiness）/ 渗透测试（出 skill 范围，是独立服务）/ 安全合规审计（独立流程）。"
metadata:
  pattern: tool-wrapper
  domain: security
  steps: 5
  composes: [code-review-gate, on-demand-guards, verification-driver, backend-development, resilience-and-observability]
---

# 安全编码规范

> **本 skill 不重复**: 部署/WAF → `release-readiness`；Auth 业务设计（JWT/OAuth）→ `backend-development` §2.3；CA/证书/HTTPS → 基础设施层。本 skill 解决"按 SOP 写出安全代码"的纪律，覆盖 OWASP Top 10 2021 + Threat Modeling + 依赖审计。

## 1. 决策树

```
任务是什么？
├─ 写新功能含用户输入/敏感数据 → §2.1 输入验证 4 步
├─ 加鉴权/权限 → §2.2 OWASP A01+A07 + RBAC/ABAC 决策
├─ 集成第三方库 → §2.3 依赖审计 + SBOM
├─ 设计威胁模型 → §2.4 STRIDE threat modeling 6 类
├─ 处理 secret/凭证 → §2.5 secret management 5 路径
└─ 漏洞响应（已知 CVE）→ §2.6 CVE triage + 修复 SOP
```

## 2. 6 路径规范

### 2.1 输入验证 4 步（信任用户为恶意）

OWASP A03: Injection 第一防线 = 验证输入。

```
输入处理（每次必跑）：
├─ 1. Allowlist（白名单）— 拒绝"未知字符"，不是"过滤已知坏字符"
│       enum / regex / schema（pydantic / zod / serde）
├─ 2. Type validation — int / string / length / range
├─ 3. Sanitization — HTML（DOMPurify / OWASP Java Encoder） / SQL（用参数化，不字符串拼接）/ Shell（避免调或参数化 exec）/ JSON schema
└─ 4. Output encoding — 输出时按上下文编码（HTML / JS / URL / SQL）
```

**Anti-pattern**：
- "我加 regex 过滤 `"` 和 `'` 就够了"
- "escape_string() 防注入"（错；参数化才防）
- "前端验证 = 后端验证"（**必须两次**；前端是 UX，后端是安全）

### 2.2 鉴权与权限（OWASP A01 A07）

**两阶段**：
- **Authentication**（你是谁）→ AuthN
- **Authorization**（你能干什么）→ AuthZ

```
AuthN 选型（按风险）：
├─ 自建用户名密码 → 仅低风险（用 argon2/bcrypt + salt + MFA）
├─ 第三方 OAuth（Google/GitHub/SSO）→ 中风险（trust IdP）
├─ Magic link / 一次性邮箱登录 → 中风险（link 短 TTL）
└─ WebAuthn / Passkey → 高风险推荐（A01 防钓鱼 + FIDO2）

AuthZ 模型：
├─ RBAC（role-based）→ 简单（admin/user）
├─ ABAC（attribute-based）→ 复杂（"经理+同部门+工时未超"）
├─ ReBAC（relationship-based）→ 协作（"文档 owner 可分享"）
└─ PBAC（policy-based）→ OPA / Cedar（声明式 policy）
```

**A01 Broken Access Control 防范**：
- **拒绝默认**：所有 endpoint 默认 deny，显式 allow
- **最小权限**：每个 role 最小权限集
- **服务端校验**：**永不** 信任前端按钮（仅显示，不鉴权）
- **资源 owner 校验**："user A 不能改 user B 的数据" — 始终校验 not just role
- **日志记录**：所有 auth/authz 决策记 log（含 user_id + 资源 + 决定）

**A07 Identification and Auth Failures 防范**：
- 密码强度（≥12 字符 + NIST 推荐 zxcvbn）
- MFA 推荐（高风险功能强制）
- 防 credential stuffing（rate limit + IP/device fingerprint）
- Session 失效（idle 30min + absolute 24h）
- Cookie 属性（HttpOnly + Secure + SameSite=Strict）

### 2.3 依赖审计 + SBOM（OWASP A06）

```
每加一个依赖必跑：
├─ 1. CVE 状态（npm audit / pip-audit / cargo audit / govulncheck）
├─ 2. 维护活跃度（last commit < 6 months）
├─ 3. 下载量（防范 typo-squatting）
├─ 4. License 兼容
└─ 5. SBOM 生成（Software Bill of Materials）
    → CycloneDX / SPDX 格式
    → 接入 SCA（Software Composition Analysis）工具
```

**CI 必跑**：
```yaml
- name: npm audit
  run: npm audit --audit-level=high
- name: govulncheck (Go)
  run: govulncheck ./...
- name: Snyk / Trivy
  run: snyk test --severity-threshold=high
```

**反模式**：
- "npm install 一把梭" 不 audit
- "依赖爆 CVE 了但我们用得对" → **所有 CVE 必须修或 explicitly accept 文档化**
- 自己写 crypto / auth / 序列化（用成熟库：libsodium / argon2 / Authlib）

### 2.4 Threat Modeling（STRIDE 6 类）

```
每新功能/新接口/新服务的 threat model：

S Spoofing            身份伪造      → 谁能冒充谁？AuthN
T Tampering           数据篡改      → 谁改什么？数据完整性
R Repudiation         否认操作      → 谁否认什么？审计日志
I Information         信息泄露      → 泄露什么？classification
D Denial of Service   拒绝服务      → 怎么耗资源？rate limit
E Elevation           权限提升      → 谁能干什么？最小权限
```

**实践**：每个 PR 拷 1 个 STRIDE 子集至少 1 类（不强求 6 类全做，迭代）。

### 2.5 Secret Management 5 路径

```
1. 不入代码 / config（用 env var + vault）
2. 不入日志（secret 值在 log filter 加 mask）
3. 不入 git（.env in .gitignore；泄漏 → 立刻 rotate + 报告 + 1password bitwarden）
4. 不入 HTTP response（API 返回不包含 secret，只给"是否设置"）
5. 入 vault 集中管理（HashiCorp Vault / AWS Secrets Manager / SOPS+age）
```

**Secret rotation**：
- API key 90 天
- DB password 60-90 天
- OAuth client secret on personnel change

**反模式**：
- secret 在 .env 文件入 git（最常见泄露源）
- 日志 print 出 config / token（自动 scrape 到日志收集系统）
- "测试环境的 secret 复用生产的"（测试人看 production DB）

### 2.6 漏洞响应（CVE / 渗透测试 / Bug Bounty）

```
收到 CVE 报告 → triage → fix → release → postmortem：

├─ 1. Triage（确认 reproducible + 严重性 + 受影响版本）
│     Critical（CVSS ≥ 9）→ 24h fix
│     High（7-9）→ 7d
│     Medium（4-7）→ 30d
│     Low（< 4）→ 下一 minor
│
├─ 2. Fix（最小化变更 + 写 fix changelog + CVE credit）
│
├─ 3. Test（写 regression test 防重 + 验证 fix）
│
├─ 4. Release（patch version bump + CVE advisory publish）
│
└─ 5. Postmortem（如果 Critical：blameless + 写 lesson 进 ADR 或 skill）
```

**内部 Found-resolved chain**：开发 → 安全 → 用户（透明披露 + 修复）。

## 3. 负向约束 + 替代方案

| 不要做 ❌ | 应该做 ✅ |
|---|---|
| 自写加密 / auth / 序列化 | 用成熟库（libsodium / jose / argon2） |
| "前端验证了后端不用" | 两端都验证（前端是 UX，后端是真安全）|
| "MD5 加盐就够了" | argon2id / bcrypt（GPU 抗性 + 内存硬） |
| Secret 写代码 commit 进 git | env var + vault + rotation |
| 字符串拼 SQL（`"SELECT ..." + name + "..."`） | 参数化查询（`SELECT ... WHERE name = ?`） |
| `eval` / `exec` 用户输入 | 不调或参数化 exec / 白名单 |
| "自己审 audit 太慢" | SAST 自动化（Snyk / CodeQL / Semgrep）+ 人工 |
| 把 user session 写服务端 file | 集中 session store（Redis / DB） |
| "我们用 HTTPS 就 secure 了" | HTTPS + HSTS + CSP + input validation + least privilege |
| "我们的金丝雀 demo 不审" | 全部环境都按 production 安全标准 |

## 4. Post-Generation 自查清单

- [ ] 所有用户输入 4 步验证（白名单/类型/sanitize/encode）
- [ ] 每 endpoint 默认 deny + 显式 allow
- [ ] 资源 owner 校验（不可只用 role）
- [ ] Secret 不在代码/log/HTTP response
- [ ] 依赖 CVE 状态审计（npm audit / govulncheck 等）
- [ ] SBOM 生成（CI 自动）
- [ ] Threat model 至少 1 类 STRIDE 覆盖
- [ ] Auth/AuthZ 决策进日志（带 user_id + 资源）
- [ ] 错误响应不泄露内部（stack trace / SQL detail）
- [ ] SAST 自动 scan（CI）
- [ ] `forge review pass`（含 security checklist）通过
- [ ] OWASP Top 10 一一对照（**至少我们写的不踩雷**）

## 5. Gotchas（实操易错点）

**G1**: "正则过滤" 输入 → 永远被绕（编码、空字节、Unicode normalize）。预防：白名单（enum/schema）而非黑名单。

**G2**: 前端 hidden field 信任 → 用户改 form 绕过。预防：权限只信服务端。

**G3**: JWT 存敏感数据 → JWT payload base64（不加密）。预防：JWT 只放 id，敏感数据查 DB。

**G4**: HTTPS only 忘加 HSTS → 中间人攻击降级。预防：Strict-Transport-Security header。

**G5**: "dependency CVE 影响小" → 真利用时发现难。预防：CVE 必 fix 或 documented risk accept。

**G6**: 密码 hashing 用 MD5/SHA1 → GPU 1 秒破解。预防：argon2id / bcrypt（计算贵 + 内存硬）。

**G7**: "API 只 GET/POST" → CSRF 攻击。预防：CSRF token / SameSite=Strict / 验证 Origin。

**G8**: "高权限错就错，反正没人知道" → 实际暴露日志可发现。预防：Auth 决策 + access log 进 SIEM。

## 6. 提交前必跑

```bash
# 1. SAST 静态扫描（自动）
snyk code test --severity-threshold=high
# 或
semgrep --config=p/owasp-top-ten src/

# 2. 依赖审计（自动）
npm audit --audit-level=high        # Node
govulncheck ./...                  # Go
pip-audit                          # Python
cargo audit                        # Rust

# 3. Secret 检测（防止 commit 进 git）
gitleaks detect --staged

# 4. 安全 checklist 跑（人工）
forge skills audit --skill=secure-coding

# 5. Code review 触发 OWASP checklist
forge review pass                   # code-review-gate 带 OWASP 子检查
```

不过 → §4 自查清单补足；过 → commit + 安全 reviewer 双 sign（高风险改动）。

## 7. 与其他 skill 的协作

- **API 设计层**：`backend-development` §2.3 鉴权层 + §2.2 endpoint 变更
- **韧性层**：`resilience-and-observability` — rate limit 也防 DDoS，security event 进 SIEM
- **依赖管理**：`system-architecture` §2.6 12-factor + SBOM 对接
- **代码审查**：`code-review-gate` — 应集成 OWASP checklist 子集
- **部署**：`release-readiness` — Secret rotation + CVE scan 准入

## 8. 安全原则 vs 用户体验平衡

| 风险高 | 行为 | 用户体验代价 |
|---|---|---|
| 支付/医疗 | MFA 强制 | 高 5 秒 |
| 一般 CRUD | 密码 + Session | 低 0 |
| 阅读类公共内容 | 无 Auth | 0 |
| Admin 后台 | MFA + IP 白名单 | 中 |
| API 集成 | OAuth + scope + rate limit | 自动化 |

**原则**：按 risk level 调节，**绝不** 全站 2FA（用户体验崩坏）。

## 9. OWASP Top 10 2021 一一映射

| OWASP 类别 | 主要防御 | 在本 skill |
|---|---|---|
| A01 Broken Access Control | deny default + owner check + audit log | §2.2 + §3 |
| A02 Cryptographic Failures | TLS + 库 + secret mgmt + 现 algorithm | §2.5 + §3 |
| A03 Injection | 白名单 + 参数化 + 输出编码 | §2.1 |
| A04 Insecure Design | Threat model + secure design pattern | §2.4 |
| A05 Security Misconfiguration | hardening + 最小权限 + 默认 secure | §3 + §4 |
| A06 Vulnerable Components | audit + SBOM + 自动更新 | §2.3 |
| A07 Auth Failures | MFA + 防 credential stuffing + Session | §2.2 |
| A08 Software/Data Integrity | 签名验证 + SLSA + code review | §2.3 + §6 |
| A09 Logging/Monitoring Failures | SIEM + alert on security event | 与 `resilience-and-observability` 联动 |
| A10 SSRF | URL 白名单 + 网络隔离 + 不传用户 URL | §2.1 |

## 参考

- 完整 references 进 `references/`（OWASP Cheat Sheet / STRIDE worksheet / SBOM 范例 / 各语言具体 advice：Go/Node/Rust/Python）
- 调研权威源：[OWASP Top 10 2021](https://owasp.org/Top10/2021/A00_2021_Introduction) / [OWASP Cheat Sheets](https://cheatsheetseries.owasp.org) / [Microsoft SDL](https://www.microsoft.com/en-us/securityengineering/sdl) / [NIST Cybersecurity Framework](https://www.nist.gov/cyberframework)
- 写法参照 `skill-authoring-standard`
- 与 `resilience-and-observability` 联动：A09 Logging 进 SIEM
