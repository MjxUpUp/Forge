# Security Policy

## 报告漏洞

**不要公开开 Issue 报告安全漏洞。**

请通过 GitHub Private Vulnerability Reporting 报告：

- 仓库页面 → **Security** 标签 → **Report a vulnerability**

或邮件联系仓库所有者（见 GitHub 个人资料）。

请在报告中包含：影响的版本、复现步骤、潜在影响范围。我们会在 72 小时内确认收到，
并与你协调修复与披露时间。

## 支持范围

只维护最新 release 版本。安全修复会以 patch 版本发布，请保持升级到最新版。

## 供应链保障

- Release 二进制由 GitHub Actions 构建（`.github/workflows/release.yml`），附 `checksums.txt`
- `checksums.txt` 带 cosign keyless 签名（Sigstore 透明日志可验证）
- 每个 tar.gz 附 SPDX SBOM（`*.sbom.json`）
- npm 包带 [provenance](https://docs.npmjs.com/generating-provenance-statements) 声明，
  可用 `npm audit signatures` 验证
