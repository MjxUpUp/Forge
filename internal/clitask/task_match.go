package clitask

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// task_match.go：`forge task resume --match` 的模糊匹配（按需拉取方案「模糊匹配」节，REQ-4/REQ-8）。
// 纯函数、无依赖、结果确定：同一查询与同一批任务永远给出同样的候选与顺序。CLI 只给候选，
// 不替用户决定——确认由 agent 用结构化提问工具完成。

// matchQueryMaxRunes caps the --match query length.
//
// matchQueryMaxRunes 限定 --match 查询的长度上限。
const matchQueryMaxRunes = 200

// matchResultCap caps how many candidates --match returns.
//
// matchResultCap 限定 --match 返回的候选数。
const matchResultCap = 10

// matchScoreFloor drops candidates scoring below it (noise floor).
//
// matchScoreFloor 是噪音下限：得分低于它的候选丢弃。
const matchScoreFloor = 40

// Tier scores, highest tier hit wins per field.
//
// 档位分：每个字段取命中的最高档。
const (
	tierExact    = 100 // 查询等于 ref
	tierPrefix   = 90  // 字段以查询开头
	tierSubstr   = 80  // 字段包含查询
	tierAllWords = 70  // 查询的每个词都出现在字段里，顺序不限
	tierTypo     = 60  // 每个查询词都在字段里有编辑距离足够近的词
	tierHanBase  = 50  // 中文二元组覆盖率 ≥ 0.5 的起点（覆盖率 1.0 时 70）
	tierSubseq   = 40  // 查询字符按序出现在字段里
)

// matchField is one searchable task field with its weight.
//
// matchField 是一个可搜索的任务字段及其权重。
type matchField struct {
	Name   string // ref | summary | branch | goal
	Text   string
	Weight float64
}

// taskMatchFields lists s's searchable fields, highest weight first.
//
// taskMatchFields 列出 s 的可搜索字段，权重从高到低：ref ×1.0、Summary ×0.9、
// Branch ×0.8、Goal ×0.6。
func taskMatchFields(s *taskpipeline.TaskState) []matchField {
	return []matchField{
		{Name: "ref", Text: s.TaskRef, Weight: 1.0},
		{Name: "summary", Text: s.Summary, Weight: 0.9},
		{Name: "branch", Text: s.Branch, Weight: 0.8},
		{Name: "goal", Text: s.Goal, Weight: 0.6},
	}
}

// validateMatchQuery trims q and rejects empty or over-long queries.
//
// validateMatchQuery 去掉 q 首尾空白，拒绝空查询与超长查询。
func validateMatchQuery(q string) (string, error) {
	q = strings.TrimSpace(q)
	if q == `` {
		return ``, fmt.Errorf("--match 关键词不能为空")
	}
	if utf8.RuneCountInString(q) > matchQueryMaxRunes {
		return ``, fmt.Errorf("--match 关键词超过 %d 个字符", matchQueryMaxRunes)
	}
	return q, nil
}

// matchTasks scores every incomplete task in groups against query and returns the top candidates.
//
// matchTasks 对 groups 里每个未完成任务按 query 打分，返回得分 ≥ 噪音下限的前
// matchResultCap 个候选：得分降序 → 最后活动时间降序 → 项目 → ref 升序。
func matchTasks(query string, groups []projectTasks) []openTaskEntry {
	var out []openTaskEntry
	for _, g := range groups {
		for _, s := range g.States {
			if s == nil || s.CompletedAt != nil {
				continue
			}
			score, field := matchScoreTask(query, s)
			if score < matchScoreFloor {
				continue
			}
			e := newOpenTaskEntry(g, s)
			e.Score, e.MatchedOn = score, field
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if !a.LastActivity.Equal(b.LastActivity) {
			return a.LastActivity.After(b.LastActivity)
		}
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		return a.Ref < b.Ref
	})
	if len(out) > matchResultCap {
		out = out[:matchResultCap]
	}
	return out
}

// matchScoreTask returns s's best weighted field score for query and the field it came from.
//
// matchScoreTask 返回 s 在 query 下的最高加权字段分，以及得分来源字段。
func matchScoreTask(query string, s *taskpipeline.TaskState) (int, string) {
	best, from := 0, ``
	for _, f := range taskMatchFields(s) {
		if f.Text == `` {
			continue
		}
		tier := fieldTier(query, f.Text, f.Name == "ref")
		score := int(math.Round(float64(tier) * f.Weight))
		if score > best {
			best, from = score, f.Name
		}
	}
	return best, from
}

// fieldTier returns the highest tier query hits in text (0 when none); isRef enables the exact tier.
//
// fieldTier 返回 query 在 text 上命中的最高档分（都不中为 0）；isRef 时启用精确档。
// 查询少于 2 个字符只走精确档——单字符的前缀、子串、子序列几乎命中一切，是噪音。
func fieldTier(query, text string, isRef bool) int {
	q, t := strings.ToLower(query), strings.ToLower(text)
	if isRef && q == t {
		return tierExact
	}
	if utf8.RuneCountInString(q) < 2 {
		return 0
	}
	if strings.HasPrefix(t, q) {
		return tierPrefix
	}
	if strings.Contains(t, q) {
		return tierSubstr
	}
	qWords, tWords := matchWords(q), matchWords(t)
	// 全词档：≥2 个词，或单个词但查询带了分隔符/标点（`resume.`、`-pull`）——剥掉标点后的词
	// 整体出现在字段里，不该掉到子序列档。单词分支要求剥标点后仍 ≥2 个字符：`a.` 只剩单字母，
	// 与单字符查询同属噪音。
	if (len(qWords) >= 2 || (len(qWords) == 1 && qWords[0] != q && utf8.RuneCountInString(qWords[0]) >= 2)) && allContained(qWords, t) {
		return tierAllWords
	}
	if typoMatch(qWords, tWords, t) {
		return tierTypo
	}
	if cov, ok := hanCoverage(q, t); ok && cov >= 0.5 {
		return tierHanBase + int(math.Round(20*(cov-0.5)/0.5))
	}
	if subsequenceMatch(q, t) {
		return tierSubseq
	}
	return 0
}

// isHan reports whether r is a CJK ideograph.
//
// isHan 报告 r 是否为汉字。
func isHan(r rune) bool { return unicode.Is(unicode.Han, r) }

// matchWords splits lower-cased s into words: each run of Latin letters/digits is a word, and so is each contiguous run of Han characters.
//
// matchWords 把已小写的 s 切成词：字母数字连续段为一词，连续汉字段也作为一个词；其余
// 字符（`-` `_` `/` 空白等）都是分隔符。汉字段不拆单字：拆了之后任意 N 个汉字只要散落在
// 字段里就能凑齐「全词」档，是噪音——汉字的模糊命中交给二元组覆盖率档。
func matchWords(s string) []string {
	var words []string
	var cur []rune
	curHan := false
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range s {
		han := isHan(r)
		if !han && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(cur) > 0 && han != curHan {
			flush()
		}
		cur = append(cur, r)
		curHan = han
	}
	flush()
	return words
}

// allContained reports whether every word occurs in text as a substring.
//
// allContained 报告每个词是否都作为子串出现在 text 里。
func allContained(words []string, text string) bool {
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// typoMatch reports whether every query word is either contained in text or, when it has ≥4 characters, within edit distance max(1, len/4) of some text word — with at least one word needing the typo allowance.
//
// typoMatch 报告是否每个查询词都满足：作为子串出现在 text 里，或（仅 ≥4 字符的词）与
// text 的某个词编辑距离 ≤ max(1, ⌊词长/4⌋)；且至少有一个词用到了错拼放宽（全部精确
// 包含的情形归全词档）。
func typoMatch(qWords, tWords []string, text string) bool {
	if len(qWords) == 0 {
		return false
	}
	usedTypo := false
	for _, w := range qWords {
		if strings.Contains(text, w) {
			continue
		}
		n := utf8.RuneCountInString(w)
		if n < 4 {
			return false
		}
		limit := n / 4
		if limit < 1 {
			limit = 1
		}
		found := false
		for _, tw := range tWords {
			// 长度差本身就是编辑距离的下界：超过阈值的词直接跳过，长 Goal 不必逐词算满 DP 表。
			if d := n - utf8.RuneCountInString(tw); d > limit || -d > limit {
				continue
			}
			if editDistance(w, tw) <= limit {
				found = true
				break
			}
		}
		if !found {
			return false
		}
		usedTypo = true
	}
	return usedTypo
}

// editDistance is the optimal-string-alignment distance between a and b (adjacent transposition costs 1).
//
// editDistance 是 a、b 间的 OSA 编辑距离（插入/删除/替换/相邻换位各计 1）——相邻换位
// 是最常见的手误（reusme → resume）。
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	n, m := len(ra), len(rb)
	d := make([][]int, n+1)
	for i := range d {
		d[i] = make([]int, m+1)
		d[i][0] = i
	}
	for j := 0; j <= m; j++ {
		d[0][j] = j
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[n][m]
}

// hanBigrams returns the set of adjacent Han-character pairs within each contiguous Han run of s.
//
// hanBigrams 返回 s 中每段连续汉字内相邻两字组成的二元组集合。
func hanBigrams(s string) map[string]bool {
	out := map[string]bool{}
	var prev rune = -1
	for _, r := range s {
		if isHan(r) {
			if prev != -1 {
				out[string([]rune{prev, r})] = true
			}
			prev = r
		} else {
			prev = -1
		}
	}
	return out
}

// hanCoverage returns the share of query's Han bigrams found in text; ok=false when query has none.
//
// hanCoverage 返回查询的汉字二元组在 text 里出现的比例；查询无二元组时 ok=false。用覆盖率
// 而非 Dice 系数：任务标题通常远长于查询，Dice 会被标题长度稀释（「接续拉取」对
// 「任务接续改为按需拉取」Dice 仅 0.33，覆盖率 0.67）。
func hanCoverage(query, text string) (float64, bool) {
	qb := hanBigrams(query)
	if len(qb) == 0 {
		return 0, false
	}
	tb := hanBigrams(text)
	hit := 0
	for b := range qb {
		if tb[b] {
			hit++
		}
	}
	return float64(hit) / float64(len(qb)), true
}

// subsequenceMatch reports whether query's letters/digits appear in order in text's letters/digits, the query has ≥3 of them, and the tightest such window spans at most 3× the query length.
//
// subsequenceMatch 报告查询的字母数字是否按序出现在 text 的字母数字里：查询至少 3 个
// 字符，且最紧窗口跨度 ≤ 查询长度 × 3（缩写 rsmpl → resume-pull；跨度上限挡住长文本里
// 碰巧按序散落的字母）。
func subsequenceMatch(query, text string) bool {
	q, t := alnumRunes(query), alnumRunes(text)
	if len(q) < 3 || len(t) < len(q) {
		return false
	}
	limit := len(q) * 3
	for start := range t {
		if t[start] != q[0] {
			continue
		}
		qi := 1
		end := start
		for ti := start + 1; ti < len(t) && qi < len(q); ti++ {
			if t[ti] == q[qi] {
				qi++
				end = ti
			}
		}
		if qi == len(q) && end-start+1 <= limit {
			return true
		}
	}
	return false
}

// alnumRunes keeps only s's letters and digits.
//
// alnumRunes 只保留 s 的字母与数字。
func alnumRunes(s string) []rune {
	var out []rune
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out = append(out, r)
		}
	}
	return out
}
