package auth

// Allowed 报告 token 是否可访问受保护资源：长度不足 8 一律拒绝。
func Allowed(token string) bool {
	if len(token) < 8 {
		return false
	}
	return true
}
