package evalkit

// golden_harvest.go — L3 golden harvest（leverage-points-landing.md L3）：只读扫描
// 本地 DataDir 已完结任务的验收考卷，机械投影成 GoldenCase 候选骨架落
// evals/forge/golden/candidates/（gitignore，永不进 VCS）。候选只是骨架——
// kind/detect 须经人工探测与策展后转正 canonical（golden 只进人工策展的红线）；
// canonical 目录零写入（guard 测试钉死）。
//
// 投影规则：每条已完结任务的验收标准 → 一个 verify-acceptance 族候选（probe 以
// 同款命令在干净 fixture 里重放考卷，baseline kind=clean——真实 defective/clean
// 由策展者实跑后翻转）；同 Run 三元组去重；--since 之前的任务跳过。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"gopkg.in/yaml.v3"
)

// HarvestResult reports one harvest pass.
type HarvestResult struct {
	Tasks      int // 扫描的已完结任务数
	Criteria   int // 扫到的验收标准条数
	Written    int // 落盘候选数（去重后）
	CandDir    string
	SkippedOld int // --since 之前完成的任务
}

// HarvestCandidates scans completed tasks under the project's DataDir (read-only)
// and writes candidate skeletons. since zero-value = no time filter.
//
// HarvestCandidates 只读扫描项目 DataDir 的已完结任务并写候选骨架。since 零值 =
// 不做时间过滤。
func HarvestCandidates(root string, since time.Time) (HarvestResult, error) {
	res := HarvestResult{CandDir: filepath.Join(root, "evals", "forge", "golden", "candidates")}
	tasksDir := filepath.Join(forgedata.DataDirFor(root), "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil // 无任务历史：零候选是诚实产出
		}
		return res, fmt.Errorf("evalkit: 读任务目录 %s: %w", tasksDir, err)
	}
	if err := os.MkdirAll(res.CandDir, 0o755); err != nil {
		return res, err
	}
	seen := map[string]bool{}
	// 已有候选不覆盖（append-only：策展者可能已标注过）。
	existing, _ := os.ReadDir(res.CandDir)
	for _, e := range existing {
		seen[strings.TrimSuffix(e.Name(), ".yaml")] = true
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tasksDir, e.Name()))
		if err != nil {
			continue // 单任务读失败跳过——harvest 是尽力而为的投影
		}
		var st taskpipeline.TaskState
		if err := json.Unmarshal(body, &st); err != nil || st.CompletedAt == nil {
			continue
		}
		res.Tasks++
		if !since.IsZero() && st.CompletedAt.Before(since) {
			res.SkippedOld++
			continue
		}
		for i, c := range st.Acceptance {
			res.Criteria++
			if c.Run == "" {
				continue // Run-less file-* 条目依赖任务 diff，重放不出干净 fixture，跳过
			}
			id := harvestID(st.TaskRef, i)
			if seen[id] {
				continue
			}
			gc := harvestSkeleton(id, st, c)
			data, err := yaml.Marshal(&gc)
			if err != nil {
				continue
			}
			path := filepath.Join(res.CandDir, id+".yaml")
			if err := os.WriteFile(path, append([]byte(harvestHeader), data...), 0o644); err != nil {
				return res, err
			}
			seen[id] = true
			names = append(names, id)
			res.Written++
		}
	}
	sort.Strings(names)
	return res, nil
}

const harvestHeader = `# HARVEST CANDIDATE（机械投影骨架，非策展用例）——L3 golden harvest 产出。
# 策展流程：① 在干净环境实跑 probe 看 verify-acceptance 的真实判定；
# ② 翻转 kind/expect（clean→defective 时 detect_any 改 exit_nonzero）；
# ③ 补 description 的语境（该考卷原本抓什么）；④ 移入上级 golden/ 目录并
# forge eval golden run 校验 + 指纹轮换。canonical 目录永不被 harvest 写入。
`

// harvestID renders a stable candidate id from task ref + criterion index.
func harvestID(ref string, idx int) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, ref)
	return fmt.Sprintf("cand-%s-%d", safe, idx)
}

// harvestSkeleton projects one acceptance criterion into a baseline-clean
// candidate whose probe replays the exam in a fresh fixture repo.
func harvestSkeleton(id string, st taskpipeline.TaskState, c taskpipeline.AcceptanceCriterion) GoldenCase {
	exp := c.Expected
	if exp == "" {
		exp = "(退出码 0)"
	}
	desc := fmt.Sprintf("[候选] 任务 %s 的验收考卷重放：%s :: %s（层级 %s）——实跑后按真实判定翻转 kind",
		st.TaskRef, c.Run, exp, taskpipeline.FormatAcceptanceTier(c.Source))
	gc := GoldenCase{
		ID:            id,
		Gate:          "verify-acceptance",
		Kind:          GoldenClean,
		Expect:        "clean",
		Description:   desc,
		Deterministic: true,
		Files: []FileSpec{
			{Path: "go.mod", Content: "module evalkit.harvest\n\ngo 1.25\n"},
		},
		ProbeArgv: []string{"sh", "-c", fmt.Sprintf(
			"git add -A && git -c user.name=g -c user.email=g@localhost -c commit.gpgsign=false commit -qm init\n"+
				`"%s" task start --ref harvest/probe --accept %q >/dev/null 2>&1`+"\n"+
				`"%s" task verify-acceptance 2>&1`, "{forge}", c.Run+" :: "+c.Expected, "{forge}")},
		DetectAny: []string{"stdout_contains:存在未通过项"},
	}
	return gc
}
