// cache.go — fingerprint-gated read cache for the pulse panel.
//
// cache.go —— pulse 面板的指纹门控读缓存。此前每个轮询周期（30s）都会把全部项目的
// 所有源（task state / checklog+toollog 归档 / act 结论 / eval run）重读重解析一遍，
// since 参数只是在全量加载后的内存过滤。本缓存只在底层文件指纹（路径+mtime+大小）
// 变化时才重新加载——stat 调用很便宜，昂贵的是 JSON 解析。
//
// 关键在于缓存落在数据加载层而非投影层：僵尸判定与 severity 是时间相关的
// （IsZombie(now)），投影仍基于缓存的原始数据每次请求现算。若缓存投影结果会冻结
// 僵尸升级——任务恰恰是在盘上「毫无变化」时才变成僵尸。
package dashboard

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/skillmetrics"
	"github.com/MjxUpUp/Forge/internal/skillsdecisions"
	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// projectData 是一个项目根的已解析原始源集。skillseval 派生结果（被动/主动计数、
// 成效）在首个 skills 请求时才惰性计算——feed/stats/projects 轮询永不为此付费。
type projectData struct {
	states       []*taskpipeline.TaskState
	checkEntries []checklog.Entry
	conclusions  []act.Conclusion

	deriveOnce sync.Once
	passive    map[string]int
	active     map[string]int
	effs       []skillmetrics.SkillEffectiveness
}

// derived 惰性记忆 skillseval 聚合。它们是指纹所覆盖文件的纯函数，故同一指纹同时
// 门控原始与派生数据。
func (d *projectData) derived(root string) {
	d.deriveOnce.Do(func() {
		if passive, _, err := skillmetrics.SkillCountsFromChecklog(root); err == nil {
			d.passive = passive
		}
		if active, _, err := skillmetrics.SkillCountsFromToollog(root); err == nil {
			d.active = active
		}
		if proj, err := forgedata.ProjectFor(root); err == nil {
			if effs, err := skillmetrics.AnalyzeEffectiveness(proj); err == nil {
				d.effs = effs
			}
		}
	})
}

// skillEvalData 是缓存的单 skill eval 快照（runs + baseline + decisions）。
type skillEvalData struct {
	runs      []skillseval.EvalRun
	baseline  skillseval.Baseline
	decisions []skillsdecisions.SkillDecision
}

// pulseCache 是进程级缓存。看板只服务一个本地用户，单个共享实例足够；键为绝对路径
// （项目根 / canonical|evalDir|skill），测试夹具（唯一 t.TempDir）天然隔离。
// loadCounts 统计真实重载次数——测试经它断言缓存命中。
type pulseCache struct {
	mu         sync.Mutex
	roots      map[string]*projectData // key: project root；value 附带其指纹
	rootFps    map[string]string
	skills     map[string]*skillEvalData // key: canonical + "|" + evalDir + "|" + skill
	skillFps   map[string]string
	loadCounts map[string]int // 观测点：key（root 或 skill key）→ 真实加载次数
}

// sharedPulseCache 支撑全部 pulse handler（它们是按请求构造的纯函数，缓存只能挂在
// 包级）。
var sharedPulseCache = newPulseCache()

func newPulseCache() *pulseCache {
	return &pulseCache{
		roots:      map[string]*projectData{},
		rootFps:    map[string]string{},
		skills:     map[string]*skillEvalData{},
		skillFps:   map[string]string{},
		loadCounts: map[string]int{},
	}
}

// stampFile 把 path 的 路径+mtime+大小 指纹写入 b；缺失文件记为 "path!absent"，使其
// 之后的「出现」同样改变指纹。
func stampFile(b *strings.Builder, path string) {
	info, err := os.Stat(path)
	if err != nil {
		b.WriteString(path)
		b.WriteString("!absent;")
		return
	}
	b.WriteString(path)
	b.WriteByte('@')
	b.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 36))
	b.WriteByte(':')
	b.WriteString(strconv.FormatInt(info.Size(), 36))
	b.WriteByte(';')
}

// projectFingerprint 给项目聚合读取的全部文件打指纹：DataDir/tasks/*.json、
// checklog*.jsonl + toollog*.jsonl（active + 归档）、act/conclusions.jsonl。
func projectFingerprint(root string) string {
	var b strings.Builder
	dataDir := forgedata.DataDirFor(root)
	if entries, err := os.ReadDir(filepath.Join(dataDir, "tasks")); err == nil {
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			stampFile(&b, filepath.Join(dataDir, "tasks", e.Name()))
		}
	} else {
		// tasks 目录不存在也记入指纹——它之后被创建必须触发失效。
		b.WriteString("tasks!absent;")
	}
	for _, pat := range []string{"checklog*.jsonl", "toollog*.jsonl"} {
		if matches, err := filepath.Glob(filepath.Join(dataDir, pat)); err == nil {
			for _, m := range matches {
				stampFile(&b, m)
			}
		}
	}
	stampFile(&b, filepath.Join(dataDir, "act", "conclusions.jsonl"))
	return b.String()
}

// loadProjectData 构建单项目的原始源集，沿用此前直接加载的单源失败语义：
// ListTaskStates 错误致命（feed 转 500），checklog / act 失败降级为空（一个坏源
// 不应让整面板空白）。
func loadProjectData(root string) (*projectData, error) {
	states, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		return nil, err
	}
	d := &projectData{states: states}
	if entries, err := checklog.LoadAllAll(root); err == nil {
		d.checkEntries = entries
	}
	if proj, err := forgedata.ProjectFor(root); err == nil {
		if cs, err := act.LoadAll(proj); err == nil {
			d.conclusions = cs
		}
	}
	return d, nil
}

// projectData 返回 pr 的缓存源集，仅指纹变化时重载。
func (c *pulseCache) projectData(pr pulseRoot) (*projectData, error) {
	fp := projectFingerprint(pr.root)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rootFps[pr.root] == fp {
		if d := c.roots[pr.root]; d != nil {
			return d, nil
		}
	}
	d, err := loadProjectData(pr.root)
	if err != nil {
		return nil, err
	}
	c.roots[pr.root] = d
	c.rootFps[pr.root] = fp
	c.loadCounts[pr.root]++
	return d, nil
}

// skillFingerprint 给单 skill 详情视图读取的三个文件打指纹：runs jsonl、
// baselines.json（跨 skill 共享——任何 skill 的 baseline 变更使全部失效）、decisions.md。
func skillFingerprint(canonical, evalDir, skill string) string {
	var b strings.Builder
	if evalDir != "" {
		stampFile(&b, skillseval.RunsFile(evalDir, skill))
		stampFile(&b, skillseval.BaselinesFile(evalDir))
	}
	if canonical != "" {
		stampFile(&b, skillsdecisions.DecisionsFile(canonical, skill))
	}
	return b.String()
}

// loadSkillEval 读单 skill 的 eval 快照；单文件失败降级为空，沿用 LoadSkillDetail
// 此前的语义（runs/baselines/decisions 缺失是 null 段落，绝不报错）。
func loadSkillEval(canonical, evalDir, skill string) *skillEvalData {
	d := &skillEvalData{}
	if evalDir != "" {
		if runs, err := skillseval.LoadRuns(evalDir, skill); err == nil {
			d.runs = runs
		}
		if bl, err := skillseval.GetBaseline(evalDir, skill); err == nil {
			d.baseline = bl
		}
	}
	if canonical != "" {
		if decisions, err := skillsdecisions.LoadDecisions(canonical, skill); err == nil {
			d.decisions = decisions
		}
	}
	return d
}

// skillEval 返回单 skill 的缓存 eval 快照，仅指纹变化时重载。调用方须先校验 skill
// （skillsfm.IsValidSkillName）——缓存键与文件路径都由它拼出。
func (c *pulseCache) skillEval(canonical, evalDir, skill string) *skillEvalData {
	key := canonical + "|" + evalDir + "|" + skill
	fp := skillFingerprint(canonical, evalDir, skill)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.skillFps[key] == fp {
		if d := c.skills[key]; d != nil {
			return d
		}
	}
	d := loadSkillEval(canonical, evalDir, skill)
	c.skills[key] = d
	c.skillFps[key] = fp
	c.loadCounts[key]++
	return d
}

// loadCount 报告 key 发生了几次真实加载（测试观测点）。
func (c *pulseCache) loadCount(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loadCounts[key]
}
