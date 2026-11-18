import io

p = "internal/cli/change_test.go"
s = io.open(p, encoding="utf-8").read()

old = """		"acceptance.md": "# 验收\\n\\n```bash\\nls /definitely/not/here\\n```\\n",
	} {"""
new = """		"acceptance.md": "# 验收\\n\\n```bash\\nls /definitely/not/here\\n```\\n",
		"clarify.md":    "# 澄清\\n\\n## 边界\\n\\n只验证证据路径的打印。\\n\\n## 非目标\\n\\n不做修复。\\n\\n人确认：2026-11-18T00:00:00Z\\n",
	} {"""
assert old in s
s = s.replace(old, new)
io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("ok")