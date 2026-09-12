package cli

// eval_rule_ledger.go — W5 门禁规则可证伪台账（最小版）。
//
// 门禁规则（阈值/检测模式/新检查）的每次变更必须带可证伪声明：改前写
// prediction（claim）+ 指向可执行 probe（golden id 对应的测试/命令），合并后
// verify 实跑该 probe 并落账 outcome——claim 与 outcome 对不上的规则变更在台账
// 里一眼可见（审查 M3 的 skillsdecisions 四元组「可证伪闭环」在门禁规则侧的
// 对称落地；呼应 AHE 的「harness 编辑带可证伪预测」）。
//
// 存储：<repo>/evals/rule-ledger.jsonl（追加式 JSONL，随库提交——规则是仓级
// 资产，台账必须与规则同仓可审计；checklog 是本机台账，承载不了仓级规则史）。
// 最小版边界：verify 只实跑 probe 并落账结论，不自动改写规则；claim 与
// outcome 的语义对照由人工评审消费（这正是「可证伪」而非「自动化验证」）。

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ruleLedgerEntry 是台账一行。Kind=claim（变更时的预测声明）/ verify（实跑
// probe 后的结论）。
type ruleLedgerEntry struct {
	TS    time.Time `json:"ts"`
	Kind  string    `json:"kind"` // claim | verify
	Rule  string    `json:"rule"`
	Claim string    `json:"claim,omitempty"`
	Probe string    `json:"probe,omitempty"` // 可执行命令（sh -c 在仓库根实跑）
	Pass  *bool     `json:"pass,omitempty"`  // verify 的实跑结论
	Verge string    `json:"verge,omitempty"` // verify 输出截断（排查用）
}

func ruleLedgerPath() string {
	return evalRepoRoot() + "/evals/rule-ledger.jsonl"
}

func loadRuleLedger(path string) ([]ruleLedgerEntry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []ruleLedgerEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e ruleLedgerEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return out, fmt.Errorf("台账行损坏（%s）: %w", path, err)
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

func appendRuleLedger(path string, e ruleLedgerEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(body, '\n')); err != nil {
		return err
	}
	// O_APPEND 单行写在 POSIX 下原子；仓库内单写者（维护者）场景，不加锁。
	return nil
}

func runEvalRuleLedger(cmd *cobra.Command, args []string) error {
	list, _ := cmd.Flags().GetBool("list")
	record, _ := cmd.Flags().GetBool("record")
	rule, _ := cmd.Flags().GetString("rule")
	claim, _ := cmd.Flags().GetString("claim")
	probe, _ := cmd.Flags().GetString("probe")
	verify, _ := cmd.Flags().GetBool("verify")

	path := ruleLedgerPath()

	if list {
		entries, err := loadRuleLedger(path)
		if err != nil {
			return fmt.Errorf("BLOCKED: %v", err)
		}
		if len(entries) == 0 {
			fmt.Println("台账为空（`forge eval rule-ledger --record --rule <id> --claim <预测> --probe <可执行命令>` 记第一条）")
			return nil
		}
		for _, e := range entries {
			switch e.Kind {
			case "claim":
				fmt.Printf("CLAIM  %s  %s  rule=%s\n         claim: %s\n         probe: %s\n", e.TS.Format("2006-01-02"), e.Kind, e.Rule, e.Claim, e.Probe)
			case "verify":
				verdict := "FAIL"
				if e.Pass != nil && *e.Pass {
					verdict = "PASS"
				}
				fmt.Printf("VERIFY %s  %s  rule=%s  probe 结论=%s\n", e.TS.Format("2006-01-02"), verdict, e.Rule, e.Verge)
			}
		}
		return nil
	}

	if record {
		if rule == "" || claim == "" || probe == "" {
			return fmt.Errorf("BLOCKED: --record 需要同时给 --rule/--claim/--probe（无 probe 的 claim 不可证伪，拒绝入账）")
		}
		e := ruleLedgerEntry{TS: time.Now(), Kind: "claim", Rule: rule, Claim: claim, Probe: probe}
		if err := appendRuleLedger(path, e); err != nil {
			return err
		}
		fmt.Printf("✅ claim 已入账（%s）：规则 %s——verify 用 `forge eval rule-ledger --verify --rule %s`\n", path, rule, rule)
		return nil
	}

	if verify {
		if rule == "" {
			return fmt.Errorf("BLOCKED: --verify 需要 --rule")
		}
		entries, err := loadRuleLedger(path)
		if err != nil {
			return fmt.Errorf("BLOCKED: %v", err)
		}
		probeCmd := ""
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].Kind == "claim" && entries[i].Rule == rule {
				probeCmd = entries[i].Probe
				break
			}
		}
		if probeCmd == "" {
			return fmt.Errorf("BLOCKED: 规则 %s 无在账 claim（先 --record）", rule)
		}
		probeRun := exec.Command("sh", "-c", probeCmd)
		probeRun.Dir = evalRepoRoot() // 探针在仓库根实跑（与 ruleLedgerPath 同语义）
		probeOut, err := probeRun.CombinedOutput()
		passed := err == nil
		verge := string(probeOut)
		if len(verge) > 400 {
			verge = verge[:400]
		}
		passVal := passed
		if err := appendRuleLedger(path, ruleLedgerEntry{TS: time.Now(), Kind: "verify", Rule: rule, Probe: probeCmd, Pass: &passVal, Verge: verge}); err != nil {
			return err
		}
		if !passed {
			return fmt.Errorf("BLOCKED: 规则 %s 的 probe 实跑未过——预测被证伪，结论已入账（%s）", rule, path)
		}
		fmt.Printf("✅ 规则 %s 的 probe 实跑通过，结论已入账（%s）\n", rule, path)
		return nil
	}

	return fmt.Errorf("BLOCKED: 需要 --record 或 --verify 或 --list 之一")
}
