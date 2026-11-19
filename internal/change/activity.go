package change

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LastActivity 返回一个 change 目录下最近一次文件修改时间。
//
// next 用它把「陈旧 change」与「手上正在做的 change」分开：一个 ready 但三天没
// 动过的 change 通常是用户已经放弃的活，不该在用户想开新任务时被重新推荐。
// 取 max(目录内文件 mtime, 该目录最近一次提交时间)：只看 mtime 的话，git clone /
// checkout / worktree add 会把陈旧 change 的文件时间刷成检出时刻，三天没动的旧活
// 会立刻变「新鲜」——而那正是这个保护最该生效的场景（换机器、换 worktree 接着干）。
// 未经提交的编辑靠 mtime 兜，检出/克隆下来的靠提交时间兜。
func LastActivity(root, id string) (time.Time, error) {
	dir := filepath.Join(root, "spec", "changes", id)
	var latest time.Time
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})
	if err != nil {
		return latest, err
	}
	if committed, ok := lastCommitTime(root, id); ok && committed.After(latest) {
		latest = committed
	}
	return latest, nil
}

// lastCommitTime 读该 change 目录最近一次提交的提交时间；没有提交或没有 git 时返回 false。
func lastCommitTime(root, id string) (time.Time, bool) {
	out, err := exec.Command("git", "-C", root, "log", "-1", "--format=%ct", "--",
		filepath.ToSlash(filepath.Join("spec", "changes", id))).Output()
	if err != nil {
		return time.Time{}, false
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}, false
	}
	return time.Unix(secs, 0), true
}

// StaleAfter 是一个 ready change 多久没动就算「陈旧」：next 不再把它当作
// 下一步，改为给新任务入口。三天足够覆盖一次周末，又短于「还记得自己做过
// 这个 change」的窗口。
const StaleAfter = 72 * time.Hour
