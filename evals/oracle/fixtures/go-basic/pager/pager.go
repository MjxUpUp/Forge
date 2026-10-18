package pager

// Page 返回第 n 页（从 0 开始），size 为每页条数，越界时截断。
func Page(items []int, size, n int) []int {
	start := n * size
	end := start + size
	if start > len(items) {
		return nil
	}
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}
