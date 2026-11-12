package change

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GateOptions 是单节点 gate 的实跑参数。
type GateOptions struct {
	Timeout time.Duration
	Now     time.Time
}

// GateTask 只跑一条任务声明的判据（acceptance 里被引用的那几条围栏）。
//
// 与 Verify 的区别只有"跑哪些命令"：判据仍然来自 acceptance.md，输出仍然
// 落成证据。节点证据**永不封印整个 change**——半个验收证明不了整件事做完了；
// 它只用来回答"这条任务现在算不算过"。
func GateTask(ctx context.Context, root, id string, taskNo int, opts GateOptions) (*Result, error) {
	if err := ValidID(id); err != nil {
		return nil, err
	}
	c, err := load(root, id, nil)
	if err != nil {
		return nil, err
	}
	rawTasks, err := os.ReadFile(filepath.Join(c.Dir, FileTasks))
	if err != nil {
		return nil, fmt.Errorf("change: 读取 %s: %w", relTo(root, filepath.Join(c.Dir, FileTasks)), err)
	}
	tasks := ParseTasks(rawTasks)
	if taskNo < 1 || taskNo > len(tasks) {
		return nil, fmt.Errorf("change: 任务序号 %d 超出范围（tasks.md 里有 %d 条）", taskNo, len(tasks))
	}
	task := tasks[taskNo-1]
	if task.Done {
		return nil, fmt.Errorf("change: 任务 %d 在 tasks.md 里已经勾选完成（要做就新起一条任务）", taskNo)
	}
	if len(task.Criteria) == 0 {
		return nil, fmt.Errorf("change: 任务 %d 没写判据编号——在任务行末尾加「（判据：acceptance #N）」才能单独 gate", taskNo)
	}

	if problems := docProblemsFor(c.Dir, c.Fast); len(problems) > 0 {
		return nil, fmt.Errorf("change: %s 的产物还不完整（%s）——补齐、删掉 %s 标记后再 gate",
			id, strings.Join(problems, "、"), scaffoldMarker)
	}
	accRaw, err := os.ReadFile(filepath.Join(c.Dir, FileAcceptance))
	if err != nil {
		return nil, fmt.Errorf("change: 读取 %s: %w", relTo(root, filepath.Join(c.Dir, FileAcceptance)), err)
	}
	acc := ParseAcceptance(accRaw)
	if len(acc.Commands) == 0 {
		return nil, fmt.Errorf("change: %s 里没有 ```bash 命令——人工清单不算可执行判据", relTo(root, filepath.Join(c.Dir, FileAcceptance)))
	}

	var plan []plannedCheck
	for _, no := range task.Criteria {
		if no < 1 || no > len(acc.Commands) {
			return nil, fmt.Errorf("change: 任务 %d 引用了 acceptance #%d，但文件里只有 %d 条围栏", taskNo, no, len(acc.Commands))
		}
		plan = append(plan, plannedCheck{Index: no, Cmd: acc.Commands[no-1]})
	}
	for _, p := range append([]plannedCheck(nil), plan...) {
		if !hasExecutableLine(p.Cmd.Body) {
			return nil, fmt.Errorf("change: acceptance #%d 只有注释/空行——注释不是判据", p.Index)
		}
		if err := lintJudge(p.Cmd.Body); err != nil {
			return nil, fmt.Errorf("change: acceptance #%d: %w", p.Index, err)
		}
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultCheckTimeout
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	evDir, err := uniqueEvidenceDir(filepath.Join(c.Dir, EvidenceDirName), now.UTC().Format("20060102T150405Z"))
	if err != nil {
		return nil, err
	}

	checks, passed, err := runPlan(ctx, root, evDir, plan, timeout)
	if err != nil {
		return nil, err
	}
	sum := Summary{
		Change:           id,
		StartedAt:        now.UTC().Format(time.RFC3339),
		AcceptanceSHA256: hashBytes(accRaw),
		Checks:           checks,
		Passed:           passed,
		// 节点 gate 不封印：它只证明这几条围栏过了，不证明整个 change 过。
		Sealed: false,
		Task:   taskNo,
		Fast:   c.Fast,
	}
	finishSummary(root, &sum)
	if err := writeSummary(evDir, sum); err != nil {
		return nil, err
	}
	return &Result{Summary: sum, EvidenceDir: evDir, Manual: acc.Manual, ManualDone: acc.ManualDone}, nil
}
