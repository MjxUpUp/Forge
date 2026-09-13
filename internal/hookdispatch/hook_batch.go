package hookdispatch

// hook_batch.go — W0.2 每-事件单入口分派本体。
//
// 痛点：同一 matcher 组内每个 hook 一条 `forge hook <name>` 接线，宿主按条目
// 逐个拉起进程——一次 Bash 调用在 PreToolUse/Bash 组上要拉 4 个 forge。batch
// 把一组收敛为一次拉起：本进程内按名册顺序递归调用 RunHook 实跑每个 hook
// （os.Stdin 换载同一 payload、stdout 逐 hook 捕获），语义与逐条拉起对齐：
//   - 首个 block（*HookBlockError）原样透传——外层 cobra 映射 exit 2 与正确
//     JSON，宿主行为与逐条模式无差；
//   - 全部 allow 时合并发射：claude 形态的 additionalContext 拼为一次注入
//     （非空才发——静默 allow 保持静默），非 JSON 输出退化为文本拼接。
// 名册从 ForgeHookSpecForProfile(ActiveProfile()) 现查——档位（W0.3）与
// 接线（ForgeHookWiring 的 batch 变换）同源，无第二份名册。
//
// hook_batch.go — W0.2 single-entry dispatch. One process runs the whole
// matcher group in roster order via recursive in-process RunHook calls (stdin
// swapped to the same payload, stdout captured per hook). Semantics match
// per-hook spawning: first *HookBlockError is forwarded verbatim (outer cobra
// maps it to exit 2 + correct JSON); all-allow merges claude
// additionalContext into a single injection (empty stays silent).

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/spf13/cobra"
)

var (
	batchEvent   string
	batchMatcher string
)

var batchHookCmd = &cobra.Command{
	Use:   "batch --event <E> [--matcher <M>]",
	Short: "W0.2 单入口分派：一次进程按名册顺序实跑一个 matcher 组的全部 hook",
	RunE:  runHookBatch,
}

func init() {
	batchHookCmd.Flags().StringVar(&batchEvent, "event", "", "宿主事件名（PreToolUse/PostToolUse/Stop/SessionStart/PostCompact/UserPromptSubmit/...）")
	batchHookCmd.Flags().StringVar(&batchMatcher, "matcher", "", "工具 matcher（与 ForgeHookWiring 登记一致；空=该事件全部 matcher）")
	batchHookCmd.Flags().StringVar(&hookAgent, "agent", "", "host agent（与 hook --agent 同一变量：单进程只执行一条命令，双绑定安全）")
	HookCmd.AddCommand(batchHookCmd)
}

func runHookBatch(cmd *cobra.Command, args []string) error {
	if batchEvent == "" {
		return fmt.Errorf(`batch 需要 --event <E>（如 PreToolUse）`)
	}
	payload, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("batch stdin 读取失败: %w", err)
	}

	names := batchHookNames(batchEvent, batchMatcher)
	if len(names) == 0 {
		// 组内无 hook（未登记/档位裁空）：静默 allow，与逐条模式的无条目语义一致。
		return nil
	}

	var outs []batchEmitted
	for _, name := range names {
		out, runErr := runHookCaptured(cmd, name, payload)
		if runErr != nil {
			// 首个 block/硬错误原样透传：外层 cobra 的错误处理（exit 2 + JSON）
			// 与逐条拉起完全一致；后续 hook 不再执行——与宿主「阻断即停」语义相同。
			return runErr
		}
		outs = append(outs, batchEmitted{name, out})
	}
	fmt.Print(mergeBatchOutputs(batchEvent, outs))
	return nil
}

// batchHookNames 按档位过滤后的名册收集 (event[, matcher]) 组内的 hook 名
// （命令形态恒为 "forge hook <name>"，出自 ForgeHookSpec 单一事实源）。
func batchHookNames(event, matcher string) []string {
	var names []string
	for ev, ms := range hooks.ForgeHookSpecForProfile(hooks.ActiveProfile()) {
		if ev != event {
			continue
		}
		for _, m := range ms {
			if matcher != "" && m.Matcher != matcher {
				continue
			}
			for _, h := range m.Hooks {
				if rest, ok := strings.CutPrefix(h.Command, "forge hook "); ok {
					if n := strings.TrimSpace(rest); n != "" {
						names = append(names, n)
					}
				}
			}
		}
	}
	return names
}

// runHookCaptured 以换载 stdin / 捕获 stdout 的方式递归实跑单个 hook。
// RunHook 的全部既有语义（档位门、checklog、attribution、in-process 特例、
// embed 脚本）原样生效——batch 只换壳不加行为。
func runHookCaptured(cmd *cobra.Command, name string, payload []byte) (string, error) {
	oldIn, oldOut := os.Stdin, os.Stdout
	inR, inW, err := os.Pipe()
	if err != nil {
		return "", err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdin, os.Stdout = inR, outW
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()

	if _, err := inW.Write(payload); err != nil {
		return "", err
	}
	if err := inW.Close(); err != nil {
		return "", err
	}
	runErr := RunHook(cmd, []string{name})
	if err := outW.Close(); err != nil {
		return "", err
	}
	b, _ := io.ReadAll(outR) // 写端已关，读到 EOF；hook 输出经 9500 rune 截断有界
	return string(b), runErr
}

// batchEmitted 是一个 hook 的捕获输出。
type batchEmitted struct {
	name string
	out  string
}

// mergeBatchOutputs 合并全部 allow 输出：claude 形态的 additionalContext 拼成
// 一次注入（宿主只解析一个 JSON 文档，逐条拼会产出非法多文档流）；非 JSON
// 输出退化为文本拼接。全空 = 静默。
func mergeBatchOutputs(event string, outs []batchEmitted) string {
	var ctxs, raws []string
	for _, o := range outs {
		s := strings.TrimSpace(o.out)
		if s == "" {
			continue
		}
		var probe map[string]any
		if err := json.Unmarshal([]byte(s), &probe); err == nil {
			if hs, ok := probe["hookSpecificOutput"].(map[string]any); ok {
				if ac, ok := hs["additionalContext"].(string); ok && strings.TrimSpace(ac) != "" {
					ctxs = append(ctxs, ac)
					continue
				}
			}
		}
		raws = append(raws, s)
	}
	var b strings.Builder
	if len(ctxs) > 0 {
		merged, _ := json.Marshal(map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName":     event,
				"additionalContext": strings.Join(ctxs, "\n\n"),
			},
		})
		b.Write(merged)
	}
	for _, r := range raws {
		b.WriteString("\n")
		b.WriteString(r)
	}
	return b.String()
}
