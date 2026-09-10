package gatecmdform

import "testing"

// 形态分类夹具取自两机审计实录（docs/design/harness-fixes-a-g-2026-09.md C、M）：
// standalone / cd&& 前缀 / && 尾段 / 分号续行 / 管道截断 / grep 掩蔽 / 多门禁连刷。

func TestClassify_Forms(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want Form
	}{
		{"standalone", `forge task gate task-verify --ref feat/x`,
			Form{Gates: 1, Standalone: true, Compliant: true}},
		{"cd prefix only", `cd E:/Forge && forge task gate task-implement --ref feat/x`,
			Form{Gates: 1, CdPrefix: true, Standalone: true, Compliant: true}},
		{"and chain tail", `go build ./... && forge task gate task-verify --ref feat/x`,
			Form{Gates: 1, AndChain: true, Compliant: true}},
		{"and chain gate not last", `forge task gate task-verify --ref feat/x && git status`,
			Form{Gates: 1, AndChain: true, Compliant: true}},
		{"semicolon continuation", `forge task gate task-verify --ref feat/x; forge task gate task-complete --ref feat/x`,
			Form{Gates: 2, Semicolon: true, MultiGate: true}},
		{"pipe truncation", `cd E:/Forge && forge task complete --ref feat/x 2>&1 | tail -5`,
			Form{Gates: 1, CdPrefix: true, PipeTruncated: true}},
		{"grep mask", `forge task complete --ref feat/x 2>&1 | grep -E "completed|Score"`,
			Form{Gates: 1, PipeTruncated: true, GrepMasked: true}},
		{"multi gate and-chain", `forge task gate task-implement --ref x && forge task gate task-verify --ref x && forge task gate task-complete --ref x`,
			Form{Gates: 3, AndChain: true, MultiGate: true}},
		{"dev binary variant", `./bin/forge-dev.exe review pass --ref feat/x`,
			Form{Gates: 1, Standalone: true, Compliant: true}},
		{"docs lint and doc-review are gate family", `forge docs lint docs/x.md && forge task doc-review --passed pass --score 90`,
			Form{Gates: 2, AndChain: true, MultiGate: true}},
		{"or-chain is not compliant", `forge task gate task-verify --ref x || echo failed`,
			Form{Gates: 1, OrChain: true}},
		{"no gate command", `go test ./... 2>&1 | tail -3`, Form{}},
		{"gate mentioned inside quotes is data", `git commit -m "run forge task gate task-verify later"`, Form{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.cmd)
			if got != c.want {
				t.Fatalf("Classify(%q)\n got %+v\nwant %+v", c.cmd, got, c.want)
			}
		})
	}
}

func TestClassify_Compliant(t *testing.T) {
	// Compliant = 含门禁命令且形态仅为 standalone / cd&& 前缀 / && 链（无 ; | || 且非多门禁）
	if !Classify(`forge task gate task-verify --ref x`).Compliant {
		t.Fatal("standalone must be compliant")
	}
	if Classify(`forge task gate task-verify --ref x | tail -1`).Compliant {
		t.Fatal("pipe-truncated must not be compliant")
	}
	if Classify(`forge task gate task-verify --ref x && forge task gate task-complete --ref x`).Compliant {
		t.Fatal("multi-gate rush must not be compliant even on && chain")
	}
	if Classify(`ls`).Compliant {
		t.Fatal("commands without a gate are out of scope (not compliant, not violating)")
	}
}
