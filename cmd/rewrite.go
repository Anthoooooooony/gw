package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gw-cli/gw/filter"
	"github.com/gw-cli/gw/shell"
	"github.com/spf13/cobra"
)

// rewriteCmd 是 Claude Code PreToolUse hook 的入口：
// 从 stdin 读入 Claude Code 的 hook JSON，按需把 Bash 命令改写成 `gw exec <cmd>`，
// 并通过 stdout 输出 hookSpecificOutput 响应告知 Claude Code 使用新命令。
var rewriteCmd = &cobra.Command{
	Use:    "rewrite",
	Short:  "Claude Code PreToolUse hook 入口（内部使用）",
	Long:   "从 stdin 读取 Claude Code hook 事件 JSON，按需把 tool_input.command 改写为 `gw exec <cmd>`。",
	Args:   cobra.NoArgs,
	Hidden: true,
	Run:    runRewrite,
}

func init() {
	rootCmd.AddCommand(rewriteCmd)
}

// hookInput 对应 Claude Code PreToolUse hook 的 stdin JSON。
// 额外字段（session_id / cwd 等）透传即可，此处只需要 tool_input。
type hookInput struct {
	ToolName  string                 `json:"tool_name"`
	ToolInput map[string]interface{} `json:"tool_input"`
}

// hookOutput 是 Claude Code 识别的"改写工具输入"响应。
// updatedInput 整体替换原 tool_input，所以要带上原 tool_input 的全部字段。
type hookOutput struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	HookEventName      string                 `json:"hookEventName"`
	PermissionDecision string                 `json:"permissionDecision"`
	UpdatedInput       map[string]interface{} `json:"updatedInput"`
}

// runRewriteWith 是可测试入口：不依赖 os.Stdin/Stdout 和 os.Exit。
// 返回 (改写后的新命令, 是否改写)；未改写时调用方应保持静默让 Claude Code 走默认流程。
// gwPath 是运行 `gw exec` 的绝对路径；本进程就是 gw，取 os.Executable() 即可，
// 外部注入以便测试。
func runRewriteWith(stdin io.Reader, stdout io.Writer, gwPath string) error {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("读取 stdin 失败: %w", err)
	}
	// 空输入：静默放行（开发时直接执行 gw rewrite 不报错）。
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}

	var in hookInput
	if err := json.Unmarshal(data, &in); err != nil {
		return fmt.Errorf("解析 hook 输入 JSON 失败: %w", err)
	}
	if in.ToolName != bashMatcher {
		return nil
	}
	command, _ := in.ToolInput["command"].(string)
	if command == "" {
		return nil
	}

	newCmd, ok := rewriteBashCommand(command, gwPath)
	if !ok {
		return nil
	}

	updated := make(map[string]interface{}, len(in.ToolInput))
	for k, v := range in.ToolInput {
		updated[k] = v
	}
	updated["command"] = newCmd

	resp := hookOutput{
		HookSpecificOutput: hookSpecificOutput{
			HookEventName:      preToolUseEvent,
			PermissionDecision: "allow",
			UpdatedInput:       updated,
		},
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}

// rewriteBashCommand 判断命令是否需要被 gw 代理；需要则返回改写后的新命令字符串。
// 逻辑沿用原实现：shell.AnalyzeCommand 拆链式命令 → 对每段 registry.Find 判断是否支持 → 前缀 gw exec。
func rewriteBashCommand(command, gwPath string) (string, bool) {
	canRewrite, segments := shell.AnalyzeCommand(command)
	if !canRewrite {
		return "", false
	}
	registry := filter.GlobalRegistry()
	gwExecPrefix := shellQuote(gwPath) + " exec "

	anyRewritten := false
	for i, seg := range segments {
		parts := strings.Fields(seg.Cmd)
		if len(parts) == 0 {
			continue
		}
		cmdName := parts[0]
		cmdArgs := parts[1:]
		if registry.Find(cmdName, cmdArgs) != nil {
			segments[i].Cmd = gwExecPrefix + seg.Cmd
			anyRewritten = true
		}
	}
	if !anyRewritten {
		return "", false
	}

	var sb strings.Builder
	for i, seg := range segments {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(seg.Cmd)
		if seg.Sep != "" {
			sb.WriteString(" ")
			sb.WriteString(seg.Sep)
		}
	}
	return sb.String(), true
}

func runRewrite(cmd *cobra.Command, args []string) {
	gwPath, err := resolveGwPath()
	if err != nil {
		// 无法解析 gw 路径时静默放行：stderr warn，stdout 不输出，让 Claude Code 走默认流程。
		fmt.Fprintf(os.Stderr, "gw rewrite: %v\n", err)
		return
	}
	if err := runRewriteWith(os.Stdin, os.Stdout, gwPath); err != nil {
		// hook 解析/写入失败 → 静默放行（不阻断用户），错误打到 stderr。
		fmt.Fprintf(os.Stderr, "gw rewrite: %v\n", err)
	}
}
