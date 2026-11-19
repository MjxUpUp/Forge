package change

import (
	"os"
	"path/filepath"
	"time"
)

// LastActivity 返回一个 change 目录下最近一次文件修改时间。
//
// next 用它把「陈旧 change」与「手上正在做的 change」分开：一个 ready 但三天没
// 动过的 change 通常是用户已经放弃的活，不该在用户想开新任务时被重新推荐。
// 只看目录内文件的 mtime，不做语义判断（谁放弃的、为什么放弃，只有人知道）。
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
	return latest, err
}

// StaleAfter 是一个 ready change 多久没动就算「陈旧」：next 不再把它当作
// 下一步，改为给新任务入口。三天足够覆盖一次周末，又短于「还记得自己做过
// 这个 change」的窗口。
const StaleAfter = 72 * time.Hour
