package nodestamp

// resetForTest drops the process singletons (tests simulate process restart).
//
// resetForTest 丢弃进程单例（测试模拟进程重启）。
func resetForTest() {
	mu.Lock()
	defer mu.Unlock()
	clock = nil
	identity = nil
	idTried = false
	identityFresh = false
	cachedHome = ``
	degradeNoted = false
	seqRestartNoted = false
}
