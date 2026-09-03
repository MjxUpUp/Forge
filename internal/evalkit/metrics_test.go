package evalkit

// metrics_test.go — 字典 fail-closed 校验与仓内资产守卫（roster 完整性）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDictionaryRepoAsset(t *testing.T) {
	// 守卫：仓内提交的 metrics.yaml 必须永远通过校验（改字典不改校验 = 红）。
	d, err := LoadDictionary(filepath.Join("..", "..", "evals", "forge", "metrics.yaml"))
	if err != nil {
		t.Fatalf("仓内 metrics.yaml 校验失败: %v", err)
	}
	if len(d.Metrics) < 15 {
		t.Fatalf("指标数异常少: %d", len(d.Metrics))
	}
	for _, c := range AllClaims {
		found := false
		for i := range d.Metrics {
			if d.Metrics[i].Claim == string(c) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("主张 %s 无指标挂靠", c)
		}
	}
}

func validTestDictionary() Dictionary {
	// 最小合法字典：C1-C7 每条主张恰好一条指标。
	claims := []string{"C1", "C2", "C3", "C4", "C5", "C6", "C7"}
	d := Dictionary{Version: 1}
	for i, c := range claims {
		d.Metrics = append(d.Metrics, MetricDef{
			ID: "m" + c, Claim: c, Track: string(TrackB), Definition: "d",
			Source: "s", MisuseNote: "m", MinSamples: 1,
		})
		_ = i
	}
	return d
}

func TestDictionaryValidateFailClosed(t *testing.T) {
	valid := validTestDictionary()
	if err := valid.Validate(); err != nil {
		t.Fatalf("合法字典不应报错: %v", err)
	}
	cases := []struct {
		name    string
		mutate  func(*Dictionary)
		wantSub string
	}{
		{"缺 misuse_note", func(d *Dictionary) { d.Metrics[0].MisuseNote = "" }, "misuse_note"},
		{"claim 不在 roster", func(d *Dictionary) { d.Metrics[0].Claim = "C9" }, "C1-C7"},
		{"track 非法", func(d *Dictionary) { d.Metrics[0].Track = "track-c" }, "track"},
		{"min_samples 非正", func(d *Dictionary) { d.Metrics[0].MinSamples = 0 }, "min_samples"},
		{"id 重复", func(d *Dictionary) {
			d.Metrics = append(d.Metrics, d.Metrics[0])
		}, "重复"},
		{"C1 失去挂靠", func(d *Dictionary) { d.Metrics[0].Claim = "C2" }, "C1"},
		{"version 非正", func(d *Dictionary) { d.Version = 0 }, "version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 每个子测试构建全新字典：Metrics 切片底层数组共享，变异会跨子测试
			// 泄漏（曾致"id 重复"用例被前序置空污染——共享可变夹具是测试缺陷，
			// 非 Validate 行为问题）。
			d := validTestDictionary()
			tc.mutate(&d)
			err := d.Validate()
			if err == nil {
				t.Fatal("期望 fail-closed 报错，得到 nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("错误信息 %q 不含 %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestLoadDictionaryMissingFile(t *testing.T) {
	_, err := LoadDictionary(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil || !strings.Contains(err.Error(), "读取指标字典失败") {
		t.Fatalf("期望读取失败错误，得到 %v", err)
	}
	_ = os.WriteFile(filepath.Join(t.TempDir(), "x.yaml"), []byte("version: 1\nmetrics: []\n"), 0o644)
}
