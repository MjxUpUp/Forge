package change

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// StaleAfter 是一个 ready change 停止推进多久之后不再被当作「下一步」。
//
// 选 72 小时：一个迭代周内的活通常有提交；超过三天没动静的 ready change 更像是
// 人已经改主意、只是没归档——把它推荐成下一步，会把新需求拖回旧活。
const StaleAfter = 72 * time.Hour

// Activities 批量返回一批 change 的「最近活动时间」。
//
// 判定口径（2026-11-19，评审 P1-a/P3-d）：
//   - 判定必须扛得住换机器：clone / worktree add 会把目录 mtime 刷成检出时间，
//     所以「干净」的 change 用**提交时间**，而不是 mtime；
//   - 有未提交改动（含未跟踪文件）时，人正在动它——取 max(文件 mtime, 提交时间)；
//   - 没有 git（或浅克隆：`git log -- path` 会返回被 graft 的 HEAD 时间）时退回
//     文件 mtime，并接受它可能偏新。
//
// 批量实现：一次 `git status --porcelain` 拿脏 change、一次 `git log --name-only`
// 拿每个 change 目录最近一次提交的时间——31 个 change 也从 31 次子进程降到 2 次。
func Activities(root string, ids []string) map[string]time.Time {
	out := make(map[string]time.Time, len(ids))
	dirty := dirtyChangeIDs(root)
	committed := lastCommitTimes(root)
	shallow := isShallowClone(root)
	now := time.Now()
	for _, id := range ids {
		dir := filepath.Join(root, "spec", "changes", id)
		mtime, hasMtime := newestMtime(dir)
		ct, hasCommit := committed[id]
		switch {
		case !hasMtime && !hasCommit:
			out[id] = now // 都不存在：别把不存在的目录判成陈旧
		case shallow || !hasCommit:
			out[id] = mtime
		case !dirty[id]:
			out[id] = ct
		default:
			if mtime.After(ct) {
				out[id] = mtime
			} else {
				out[id] = ct
			}
		}
	}
	return out
}

// LastActivity 返回单个 change 的最近活动时间（Activities 的单个版本）。
func LastActivity(root, id string) (time.Time, error) {
	return Activities(root, []string{id})[id], nil
}

// dirtyChangeIDs 返回 spec/changes 下有未提交改动（含未跟踪）的 change id。
func dirtyChangeIDs(root string) map[string]bool {
	out := map[string]bool{}
	cmd := exec.Command("git", "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", "spec/changes")
	cmd.Dir = root
	raw, err := cmd.Output()
	if err != nil {
		return out
	}
	for _, rec := range strings.Split(string(raw), "\x00") {
		if len(rec) < 4 {
			continue
		}
		p := rec[3:]
		p = strings.TrimPrefix(p, "spec/changes/")
		if i := strings.IndexByte(p, '/'); i > 0 {
			out[p[:i]] = true
		}
	}
	return out
}

// lastCommitTimes 返回 spec/changes 下每个 change 目录最近一次提交的时间。
//
// 一次 `git log --name-only` 即可：输出按提交从新到旧，第一次见到某个 change 的
// 文件时那个提交就是它最近的活动。
func lastCommitTimes(root string) map[string]time.Time {
	out := map[string]time.Time{}
	cmd := exec.Command("git", "log", "--format=C%ct", "--name-only", "-z", "--", "spec/changes")
	cmd.Dir = root
	raw, err := cmd.Output()
	if err != nil {
		return out
	}
	var ts time.Time
	for _, rec := range strings.Split(string(raw), "\x00") {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		if strings.HasPrefix(rec, "C") {
			if n, err := parseInt64(rec[1:]); err == nil {
				ts = time.Unix(n, 0)
			}
			continue
		}
		p := strings.TrimPrefix(rec, "spec/changes/")
		if i := strings.IndexByte(p, '/'); i > 0 {
			if _, ok := out[p[:i]]; !ok && !ts.IsZero() {
				out[p[:i]] = ts
			}
		}
	}
	return out
}

// newestMtime 返回目录里最新文件的修改时间（目录自身 mtime 不算：checkout 只动文件）。
func newestMtime(dir string) (time.Time, bool) {
	var newest time.Time
	found := false
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !found || info.ModTime().After(newest) {
			newest, found = info.ModTime(), true
		}
		return nil
	})
	return newest, found
}

// isShallowClone 判断是否是浅克隆：那里 `git log -- path` 会返回被 graft 的 HEAD
// 时间，把「别的文件的新提交」误算成本 change 的活动。
func parseInt64(raw string) (int64, error) {
	var n int64
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, os.ErrInvalid
		}
		n = n*10 + int64(r-'0')
	}
	return n, nil
}

func isShallowClone(root string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-shallow-repository")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}
