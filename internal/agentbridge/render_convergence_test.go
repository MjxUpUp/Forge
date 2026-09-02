package agentbridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/skillgen"
)

// TestProtocolRenderConvergence 为共享 protocol.Render* helper 在保留的渲染器里
// 真实接线（有渲染产物，非旁路）钉住。渲染器宿主迁移史：cursor 臂随 buildCursorMDC
// 移除（死代码）；windsurf 臂随 P3 指针化移除（用户级 global_rules 只承载指针，
// 不再拷贝 standards——spec-kit 共识）；现锚定 skillgen 的 forge-quality skill
//（标准清单的受管通道载体，项目级与用户级同一生成器）。
func TestProtocolRenderConvergence(t *testing.T) {
	home := t.TempDir()
	skillsRoot := filepath.Join(home, "skills", "forge-quality")
	if err := skillgen.GenerateUserQualitySkillTo(filepath.Dir(skillsRoot), protocol.DefaultProtocol()); err != nil {
		t.Fatalf("skillgen GenerateUserQualitySkillTo: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(skillsRoot, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	content := string(data)

	// 渲染产物在场（标准清单 + 会话规则列表）——共享 helper 真实接线、非旁路。
	if !strings.Contains(content, "编译") || !strings.Contains(content, "- ") {
		t.Errorf("质量标准渲染产物缺失——共享 helper 可能未接线")
	}
}
