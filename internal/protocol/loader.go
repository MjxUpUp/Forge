package protocol

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// pathFor 解析项目根 dir 的生效 protocol.yml 路径：
//   - <dir>/.forge/protocol.yml 文件存在时——团队共享覆盖层（git tracked、
//     用户可改、由 `forge init --project` 写入）；
//   - 否则用户级 DataDir 副本（零项目写入默认）。
//
// DataDir 无法解析时（FORGE_DATA_HOME 未设且用户主目录不可用）返回错误：
// 若回落成裸 "protocol.yml" 会写进进程 cwd——静默错位，文件丢在 forge 恰好
// 运行的目录。
func pathFor(dir string) (string, error) {
	projectLevel := filepath.Join(dir, ".forge", "protocol.yml")
	if info, err := os.Stat(projectLevel); err == nil && !info.IsDir() {
		return projectLevel, nil
	}
	dd, err := dataDirFor(dir)
	if err != nil {
		return ``, err
	}
	return filepath.Join(dd, "protocol.yml"), nil
}

// dataDirFor 解析 dir 的用户级 DataDir；forgedata.DataDirFor 返 ""（GlobalHome
// 失败）时 fail-fast，不让调用方拼出相对路径。
func dataDirFor(dir string) (string, error) {
	dd := forgedata.DataDirFor(dir)
	if dd == `` {
		return ``, fmt.Errorf("protocol: cannot resolve user-level DataDir (FORGE_DATA_HOME unset and user home unavailable); refusing to use a relative protocol.yml path")
	}
	return dd, nil
}

// Configured reports whether an effective protocol.yml exists (project-level override
// first, then the user-level DataDir copy). Load 的「缺文件」与「坏文件」要分开处理：
// 契约仓库缺文件是常态、可以静默回退默认；文件存在却解析失败则任何仓库都要告警。
//
// Configured 只问存在性，不解析内容——解析仍走 Load。
func Configured(dir string) bool {
	path, err := pathFor(dir)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Load reads the effective protocol.yml for the project (project-level override first, then the user-level DataDir copy).
//
// Load 读项目的生效 protocol.yml（项目级覆盖优先，其次用户级 DataDir 副本）。
// unmarshal 之后跑语义后校验（validateWarn）：severity 规范化/告警、漏写 enabled
// 的 standard 一次性 stderr 提示——为何只告警不报错见 validateWarn。
func Load(dir string) (*Protocol, error) {
	path, err := pathFor(dir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("protocol.yml not found: run 'forge init' first")
		}
		return nil, fmt.Errorf("failed to read protocol.yml: %w", err)
	}
	var p Protocol
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to parse protocol.yml: %w", err)
	}
	p.validateWarn(data)
	return &p, nil
}

// validSeverities 是所有消费方（types.go 的 Standard 注释、按 == "error" 精确比较的
// 各门禁）共同识别的小写 severity 闭合集。
var validSeverities = map[string]bool{"info": true, "warning": true, "error": true}

// validateWarn 是 Load 后的语义校验。刻意 stderr 告警而非让 Load 失败：
// protocol.yml 几乎被所有命令读取（status/门禁），语义笔误不应把它们全部
// 砸挂——文件仍是结构合法的 YAML。两项检查：
//
//  1. severity：空白/大小写规范化进小写集合（"ERROR" → "error"，同时告警提醒
//     改源头）；仍落在集合外的值保留原值并告警。未知 severity 之所以要管，是
//     消费方按 == "error" 精确比较——笔误会静默改变门禁行为。
//
//  2. enabled：YAML bool 在类型化结构里无法区分「键缺失」与「显式 false」，
//     因此用 *bool 影子结构重读原始字节。任何漏写该键的 standard 触发一次
//     聚合 stderr 提示——漏写的 enabled 解码为 false，standard 静默不生效
//     （漏写将不生效），看起来就像 forge 无视用户的 protocol。
func (p *Protocol) validateWarn(raw []byte) {
	for i := range p.Standards {
		s := &p.Standards[i]
		orig := s.Severity
		sev := strings.ToLower(strings.TrimSpace(orig))
		if validSeverities[sev] {
			if sev != orig {
				s.Severity = sev
				fmt.Fprintf(os.Stderr, "warn: standard %q 的 severity %q 已规范化为 %q（请改用小写 info/warning/error）\n", s.ID, orig, sev)
			}
			continue
		}
		fmt.Fprintf(os.Stderr, "warn: standard %q 的 severity %q 不在 info/warning/error 集合内，已保留原值——消费方按 == \"error\" 精确比较，该档不会命中；请修正 protocol.yml\n", s.ID, orig)
	}
	// 用 *bool 影子重读：nil = 键缺失（区别于显式 false）。原始字节在 Load 已
	// 解析成功，这里出错实际不可达——静默跳过，避免重复报告解析问题。
	var shadow struct {
		Standards []struct {
			Enabled *bool `yaml:"enabled"`
		} `yaml:"standards"`
	}
	if err := yaml.Unmarshal(raw, &shadow); err != nil {
		return
	}
	missing := 0
	for _, s := range shadow.Standards {
		if s.Enabled == nil {
			missing++
		}
	}
	if missing > 0 {
		fmt.Fprintf(os.Stderr, "warn: %d 个 standard 漏写 enabled 字段——漏写将不生效（YAML 缺省为 false）；请显式写 enabled: true 或 enabled: false\n", missing)
	}
}
