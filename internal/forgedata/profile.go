package forgedata

// GlobalProfile() is the path of the global wiring-profile file (~/.forge/profile).
// The file holds one bare token (lite|standard|full). Deliberately a plain file,
// not JSON: it is read by every forge hook spawn via the profile gate, so the
// read path must stay a single os.ReadFile + trim. IO lives in the hooks package
// (forgedata stays a pure path/key package by design — see package comment).
//
// GlobalProfile() 是全局接线档位文件路径（~/.forge/profile）。内容为裸 token
// （lite|standard|full）。刻意用纯文本而非 JSON：每次 forge hook 拉起都会经档位
// 门读它，读路径必须保持单次 os.ReadFile + trim。IO 在 hooks 包（forgedata 按
// 包注释设计保持纯路径/key 包）。
func GlobalProfile() string {
	home, err := GlobalHome()
	if err != nil {
		return "" // RootDir 同款：home 不可解析时空串，读侧回落默认档
	}
	return home + "/profile"
}
