package cliskills

// skills_inventory.go — `forge skills inventory`（focus-batches §D2，AST10 对齐）：
// OWASP Agentic Skills Top 10 的缓解措施钦定 "skill inventories"、"immutable
// pinning, hash verification"（AST07/AST09）——本命令把两件事落成机械可验的清单：
//   - 默认：枚举 canonical 树（skills/ + skills-forge/）+ 各 plugin pack 树，
//     每个 skill 输出 SKILL.md 的 sha256 内容指纹（inventory）
//   - --lock：把当前清单钉进 skills-inventory.lock（提交进仓 = 团队/CI 锚点）
//   - --verify：对照锁文件逐项核对 hash，漂移（skill 被改/被换/新增未知）即
//     exit 2——CI 消费形态：锁文件过期或内容漂移都过不去
//
// 边界：hash 只覆盖 SKILL.md（行为契约本体）；references/scripts 漂移由 skills
// drift-check 既有机制管。pack 树纳入清单（2026-09 拆包后设计族在
// plugins/forge-design）——AST10 关心的是"装了什么"，与树的位置无关。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/spf13/cobra"
)

func init() {
	skillsInventoryCmd.Flags().Bool("lock", false, "把当前清单写入 skills-inventory.lock（钉基线）")
	skillsInventoryCmd.Flags().Bool("verify", false, "对照 skills-inventory.lock 核对（漂移 exit 2）")
	skillsInventoryCmd.Flags().Bool("json", false, "JSON 输出")
	Root.AddCommand(skillsInventoryCmd)
}

var skillsInventoryCmd = &cobra.Command{
	Use:   "inventory [--lock | --verify] [--json]",
	Short: "skill 清单与内容指纹（AST10 对齐：inventories + hash pinning）",
	Long: `forge skills inventory 枚举 canonical 树（skills/ + skills-forge/）与各
plugin pack 树的 skill，输出每个 SKILL.md 的 sha256 内容指纹。

--lock    钉基线：清单写入 skills-inventory.lock（提交进仓即团队/CI 锚点）
--verify  对照锁文件核对：hash 漂移/未知新增/清单缺项 → exit 2
          （OWASP AST07 "immutable pinning, hash verification" 的机械落地）`,
	RunE: runSkillsInventory,
}

// inventoryItem 是清单中的一条（skill 名在树内唯一；tree 区分来源树）。
type inventoryItem struct {
	Name string `json:"name"`
	Tree string `json:"tree"`   // canonical | forge | pack:<name>
	Path string `json:"path"`   // 仓内相对路径（SKILL.md）
	Hash string `json:"sha256"` // SKILL.md 内容指纹
}

// inventoryLockFile 是锁文件位置（仓根，提交进仓）。
const inventoryLockFile = "skills-inventory.lock"

func runSkillsInventory(cmd *cobra.Command, args []string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	items, err := buildInventory(root)
	if err != nil {
		return err
	}
	lock, _ := cmd.Flags().GetBool("lock")
	verify, _ := cmd.Flags().GetBool("verify")
	asJSON, _ := cmd.Flags().GetBool("json")

	if lock {
		body, err := renderInventory(items)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(root, inventoryLockFile), []byte(body), 0o644); err != nil {
			return err
		}
		fmt.Printf("✅ 已钉基线：%d 个 skill → %s（提交进仓即团队/CI 锚点）\n", len(items), inventoryLockFile)
		return nil
	}
	if verify {
		return verifyInventory(root, items)
	}
	if asJSON {
		body, _ := json.MarshalIndent(items, "", "  ")
		fmt.Println(string(body))
		return nil
	}
	fmt.Printf("skill 清单：%d 个（--lock 钉基线 / --verify 核对）\n", len(items))
	for _, it := range items {
		fmt.Printf("  %-12s %-42s %s…\n", it.Tree, it.Name, it.Hash[:12])
	}
	return nil
}

// buildInventory 扫 canonical 两棵树 + plugins/*/skills pack 树。
func buildInventory(root string) ([]inventoryItem, error) {
	var items []inventoryItem
	trees := []struct{ dir, label string }{
		{filepath.Join(root, "skills"), "canonical"},
		{filepath.Join(root, "skills-forge"), "forge"},
	}
	for _, t := range trees {
		found, err := scanSkillTree(root, t.dir, t.label)
		if err != nil {
			return nil, err
		}
		items = append(items, found...)
	}
	packs, _ := filepath.Glob(filepath.Join(root, "plugins", "*", "skills"))
	for _, p := range packs {
		label := "pack:" + filepath.Base(filepath.Dir(p))
		found, err := scanSkillTree(root, p, label)
		if err != nil {
			return nil, err
		}
		items = append(items, found...)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Tree != items[j].Tree {
			return items[i].Tree < items[j].Tree
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func scanSkillTree(root, dir, label string) ([]inventoryItem, error) {
	var out []inventoryItem
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillMD := filepath.Join(dir, e.Name(), "SKILL.md")
		data, err := os.ReadFile(skillMD)
		if err != nil {
			continue // 退役存档目录（仅 decisions.md）不是在役 skill
		}
		rel, err := filepath.Rel(root, skillMD)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		out = append(out, inventoryItem{
			Name: e.Name(), Tree: label,
			Path: filepath.ToSlash(rel), Hash: hex.EncodeToString(sum[:]),
		})
	}
	return out, nil
}

func renderInventory(items []inventoryItem) (string, error) {
	var b strings.Builder
	b.WriteString("# forge skills inventory — AST10 hash pinning（forge skills inventory --lock 再生成）\n")
	for _, it := range items {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", it.Tree, it.Name, it.Hash, it.Path)
	}
	return b.String(), nil
}

// verifyInventory 三态差集：漂移（hash 变）/未知新增（锁外 skill）/缺项（锁内
// skill 消失）。任一非空 → exit 2（走 errors 语义：打印后返回硬失败）。
func verifyInventory(root string, current []inventoryItem) error {
	body, err := os.ReadFile(filepath.Join(root, inventoryLockFile))
	if os.IsNotExist(err) {
		return fmt.Errorf("BLOCKED: 无 %s——先 forge skills inventory --lock 钉基线并提交", inventoryLockFile)
	}
	if err != nil {
		return err
	}
	locked := map[string]inventoryItem{}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 || fields[0] == "#" || strings.HasPrefix(line, "#") {
			continue
		}
		locked[fields[1]] = inventoryItem{Tree: fields[0], Name: fields[1], Hash: fields[2], Path: fields[3]}
	}
	var drifted, unknown, missing []string
	for _, cur := range current {
		key := cur.Name
		if lk, ok := locked[key]; ok {
			if lk.Hash != cur.Hash {
				drifted = append(drifted, fmt.Sprintf("%s（%s → %s）", cur.Name, lk.Hash[:12], cur.Hash[:12]))
			}
		} else {
			unknown = append(unknown, cur.Name+"（"+cur.Tree+"）")
		}
	}
	currentSet := map[string]bool{}
	for _, c := range current {
		currentSet[c.Name] = true
	}
	for name := range locked {
		if !currentSet[name] {
			missing = append(missing, name)
		}
	}
	if len(drifted)+len(unknown)+len(missing) == 0 {
		fmt.Printf("✅ %d 个 skill 全部与锁文件一致（AST10 pinning 验证通过）\n", len(current))
		return nil
	}
	for _, d := range drifted {
		fmt.Printf("  ⚠ 漂移：%s\n", d)
	}
	for _, u := range unknown {
		fmt.Printf("  ⚠ 锁外新增：%s\n", u)
	}
	for _, m := range missing {
		fmt.Printf("  ⚠ 锁内缺项：%s\n", m)
	}
	return fmt.Errorf("BLOCKED: skills 清单与 %s 不一致（漂移 %d / 未知 %d / 缺项 %d）——审阅后 forge skills inventory --lock 重钉（AST07 update-drift 语义）",
		inventoryLockFile, len(drifted), len(unknown), len(missing))
}

// repoRoot 解析仓根：projectroot（forge 项目）优先，git 顶层兜底（AST10 验证的
// CI 形态可能在未 forge init 的 checkout 里跑——inventory 不应强制 init）。
func repoRoot() (string, error) {
	if root, err := projectroot.Find(); err == nil {
		return root, nil
	}
	cwd, _ := os.Getwd()
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return cwd, nil // 非 git：cwd 兜底（个人 skills 目录场景）
	}
	return strings.TrimSpace(string(out)), nil
}
