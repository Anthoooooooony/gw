// Package pytest 是 pytest 命令的语义感知过滤器。
//
// 设计哲学（对齐 rtk 的专属 cmd 模块）：**parse 出结构，按结构重新生成摘要**，
// 而不是靠正则 strip 原文行。parse 不到稳定锚点时返回 nil / 原文透传，绝不
// 盲目删行。
//
// 锚点：
//   - 最终汇总行形如 `==== N passed in T.TTs ====` / `= 1 failed, 2 passed ... =`，
//     pytest 稳定在最后一行打印。找不到就说明不是 pytest 标准输出（可能被
//     插件改过 / 被用户用 `tee` 截断），直接透传。
//   - 失败场景额外锚点 `=== FAILURES ===`，从该行起到结尾完整保留（assert 细节、
//     traceback、short test summary info 都在里面）。
//
// 输入来自 Claude Code 的 Bash 子进程，最多按行扫两遍，延迟可忽略。
package pytest

import (
	"regexp"
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&Filter{})
}

// Filter 是无状态的 pytest 过滤器（实例在 registry 间共享）。
type Filter struct{}

// Name 返回静态名称，供 FilterUsed 展示（"pytest"、或 "pytest/module"）。
func (f *Filter) Name() string { return "pytest" }

// Match 识别 `pytest ...` 和 `python -m pytest ...` 两种调用。
//
// 只看命令名 + 位置参数里是否出现 pytest，不尝试识别用户自定义 wrapper（如
// `tox -e pytest`）——这类 wrapper 的输出结构未知，硬匹配风险大。
func (f *Filter) Match(cmd string, args []string) bool {
	if cmd == "pytest" {
		return true
	}
	if cmd == "python" || cmd == "python3" {
		// `-m pytest` 或 `-mpytest`。宽松匹配，后续再加 args 也不影响。
		for i, a := range args {
			if a == "-m" && i+1 < len(args) && args[i+1] == "pytest" {
				return true
			}
			if a == "-mpytest" {
				return true
			}
		}
	}
	return false
}

// Subname 告诉 registry 本次匹配的 "子名"，用于 FilterUsed 展示。
func (f *Filter) Subname(cmd string, args []string) string {
	if cmd == "pytest" {
		return "pytest"
	}
	return "python -m pytest"
}

// finalSummaryRe 匹配 pytest 最终汇总行：`==== 99 passed in 0.08s ====`、
// `== 2 failed, 97 passed in 0.13s ==`、`= 1 failed, 2 passed, 1 skipped in 3.21s =` 等。
// 关键锚点：行首行尾成对的 "=" + 包含 "in <num>s"。
var finalSummaryRe = regexp.MustCompile(`^=+ .* in \d[\d.]*s =+$`)

// failuresHeaderRe 匹配 `=== FAILURES ===` 分隔线。
var failuresHeaderRe = regexp.MustCompile(`^=+ FAILURES =+$`)

// findFinalSummary 从后向前扫描找到最终 summary 行；未找到返回 ""。
// 从后向前是因为整个输出里 `== ... ==` 分隔线很多（如 "test session starts"），
// 真正的 summary 稳定在最后一行。
func findFinalSummary(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if finalSummaryRe.MatchString(strings.TrimRight(lines[i], "\r")) {
			return lines[i]
		}
	}
	return ""
}

// findFailuresStart 返回 `=== FAILURES ===` 行的索引；未找到返回 -1。
func findFailuresStart(lines []string) int {
	for i, line := range lines {
		if failuresHeaderRe.MatchString(strings.TrimRight(line, "\r")) {
			return i
		}
	}
	return -1
}

// Apply 处理成功场景（exit==0）：只保留最终 summary 行。
// 无法 parse 出 summary 时返回原文，让调用方看到完整输出——保守优先。
func (f *Filter) Apply(input filter.FilterInput) filter.FilterOutput {
	content := input.Stdout
	lines := strings.Split(content, "\n")
	summary := findFinalSummary(lines)
	if summary == "" {
		return filter.FilterOutput{Content: content, Original: content}
	}
	return filter.FilterOutput{Content: summary + "\n", Original: content}
}

// ApplyOnError 处理失败场景（exit!=0）：保留从 FAILURES 节起到末尾的全部内容。
// 该范围包含所有失败的 traceback、assert 细节、short test summary info、最终
// summary——是 LLM 诊断失败所需的最小完整集。
//
// 缺任一锚点（无 summary 或无 FAILURES 节）都返回 nil，让上层透传原文。
// 这避免了"用户用了 pytest 插件把输出结构改乱"之类的长尾场景被误删。
func (f *Filter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	content := input.Stdout + input.Stderr
	lines := strings.Split(content, "\n")
	summary := findFinalSummary(lines)
	if summary == "" {
		return nil
	}
	start := findFailuresStart(lines)
	if start < 0 {
		// pytest 报告了 N failed 但找不到 FAILURES 节——可能是 `--tb=no` 之类
		// 抑制了详情。只保留最终 summary 仍然比透传原文信息密度高，但这里
		// 保守返回 nil 更安全（用户可能恰好需要那些被 `--tb=no` 保留的上下文）。
		return nil
	}
	failBlock := strings.Join(lines[start:], "\n")
	return &filter.FilterOutput{Content: failBlock, Original: content}
}
