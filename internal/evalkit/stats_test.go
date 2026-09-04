package evalkit

// stats_test.go — 统计计算的固定输入输出黄金值（数学段逐段钉住）。

import (
	"reflect"

	"math"
	"testing"
)

func TestWilsonInterval(t *testing.T) {
	lo, hi := WilsonInterval(0, 10)
	if lo != 0 || hi <= 0 || hi >= 0.35 {
		t.Fatalf("0/10 的 Wilson 区间异常: [%f,%f]", lo, hi)
	}
	lo, hi = WilsonInterval(5, 10)
	if math.Abs(lo-0.2366) > 0.01 || math.Abs(hi-0.7634) > 0.01 {
		t.Fatalf("5/10 的 Wilson 区间偏离文献值: [%f,%f]", lo, hi)
	}
	lo, hi = WilsonInterval(3, 0)
	if lo != 0 || hi != 0 {
		t.Fatalf("零试次应返回 (0,0): [%f,%f]", lo, hi)
	}
}

func TestCohenKappa(t *testing.T) {
	k, err := CohenKappa([]string{"a", "a", "b", "b"}, []string{"a", "a", "b", "b"})
	if err != nil || math.Abs(k-1) > 1e-9 {
		t.Fatalf("完全一致 k 应为 1: %v %v", k, err)
	}
	// 真正的退化：单一类别完全一致（expected=1，分母 0）→ κ 无定义，
	// 返回错误而非满分（对抗审查 M8）。注意 [a,b] vs [a,b] 是合法输入 κ=1
	//（observed=1、expected=0.5）——"同边际"不等于"退化"，勿混淆。
	if k2, err2 := CohenKappa([]string{"a", "a"}, []string{"a", "a"}); err2 == nil {
		t.Fatalf("单类别完全一致应报无定义错误: %v", k2)
	}
	k, err = CohenKappa([]string{"a", "a", "b", "b"}, []string{"a", "b", "a", "b"})
	if err != nil || math.Abs(k) > 1e-9 {
		t.Fatalf("50%% 一致的对称矩阵 k 应为 0: %v %v", k, err)
	}
	if _, err := CohenKappa([]string{"a"}, nil); err == nil {
		t.Fatal("空/不等长输入应报错")
	}
}

func TestReplayAgreement(t *testing.T) {
	a, d := ReplayAgreement([]string{"x", "x", "y"})
	if math.Abs(a-2.0/3.0) > 1e-9 || d != 2 {
		t.Fatalf("期望 2/3 与 2 个不同判定: %f %d", a, d)
	}
	a, d = ReplayAgreement(nil)
	if a != 0 || d != 0 {
		t.Fatalf("空输入应为 (0,0): %f %d", a, d)
	}
}

// golden 网格直接取自 arXiv 2605.23950 §4.2 的 3×3 实验数值——数学段与文献对表。
// 模型名 glm/gpt/kimi 是论文公开发表的实验对象（文献引用，非维护者技术栈——
// 脱敏纪律不适用于学术对表数据）。
func TestDecomposeVariancePaperGrid(t *testing.T) {
	models := []ModelScores{
		{Name: "glm", ScoresByHarness: map[string]float64{"H1": 52.5, "H2": 56.5, "H3": 65.5}},
		{Name: "gpt", ScoresByHarness: map[string]float64{"H1": 55.0, "H2": 58.5, "H3": 63.5}},
		{Name: "kimi", ScoresByHarness: map[string]float64{"H1": 52.0, "H2": 59.0, "H3": 60.5}},
	}
	dec, err := DecomposeVariance(models)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(dec.HVPerModel[0]-29.56) > 0.05 {
		t.Fatalf("GLM HV 应为 29.56: %f", dec.HVPerModel[0])
	}
	if math.Abs(dec.MVPerHarness[0]-1.72) > 0.05 {
		t.Fatalf("H1 MV 应为 1.72: %f", dec.MVPerHarness[0])
	}
	if math.Abs(dec.HvOverMv-7.80) > 0.1 {
		t.Fatalf("HV/MV 比值应为 7.80: %f", dec.HvOverMv)
	}
	if dec.Reversals != 6 || dec.PairComparisons != 9 {
		t.Fatalf("翻转应为 6/9: %d/%d", dec.Reversals, dec.PairComparisons)
	}
	// η²_p 已移除：单观测格下恒 0.5 的废数（对抗审查 C2）——结构上不允许再出现。
	if _, has := anyField(dec, "EtaSquaredP"); has {
		t.Fatal("EtaSquaredP 不应存在于分解结果")
	}
}

// anyField 反射探测字段名（仅测试用——钉住"废统计量不得回归"）。
func anyField(v any, name string) (any, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return nil, false
	}
	f := rv.FieldByName(name)
	if !f.IsValid() {
		return nil, false
	}
	return f.Interface(), true
}

func TestDecomposeVarianceDegenerateGrid(t *testing.T) {
	// 单任务/同分网格：MV=0 → 比值未定义（哨兵语义而非 +Inf——+Inf 不可 JSON
	// 序列化，docker decompose 首跑实测 2026-09-04）。
	models := []ModelScores{
		{Name: "a", ScoresByHarness: map[string]float64{"off": 1, "full": 1}},
		{Name: "b", ScoresByHarness: map[string]float64{"off": 1, "full": 1}},
	}
	dec, err := DecomposeVariance(models)
	if err != nil {
		t.Fatal(err)
	}
	if !dec.HvOverMvUndefined || dec.HvOverMv != 0 {
		t.Fatalf("退化网格应标 undefined: %+v", dec)
	}
	if math.IsInf(dec.HvOverMv, 0) {
		t.Fatal("不得返回 Inf（JSON 序列化失败）")
	}
}

func TestDecomposeVarianceErrors(t *testing.T) {
	oneModel := []ModelScores{{Name: "a", ScoresByHarness: map[string]float64{"p": 1, "q": 2}}}
	if _, err := DecomposeVariance(oneModel); err == nil {
		t.Fatal("单模型应报错")
	}
	twoModels := append(oneModel, ModelScores{Name: "b", ScoresByHarness: map[string]float64{"p": 2}})
	if _, err := DecomposeVariance(twoModels); err == nil {
		t.Fatal("单 harness 应报错")
	}
	broken := []ModelScores{
		{Name: "a", ScoresByHarness: map[string]float64{"p": 1, "q": 2}},
		{Name: "b", ScoresByHarness: map[string]float64{"p": 2}},
	}
	if _, err := DecomposeVariance(broken); err == nil {
		t.Fatal("格数不一致应报错")
	}
}
