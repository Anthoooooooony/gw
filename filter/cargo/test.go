// Package cargo 是 cargo 子命令的语义感知过滤器集合。
//
// 设计哲学（对齐 rtk 的专属 cmd 模块，参见 filter/pytest）：**按稳定锚点切片**，
// 而不是靠正则 strip 原文行。锚点缺失就透传原文，绝不盲删。
//
// 当前仅覆盖 `cargo test`。后续 `cargo build/check/clippy` 等作为独立 Filter
// 加入同包（issue #74）。
package cargo

import (
	"regexp"
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&TestFilter{})
}

// TestFilter 是无状态的 `cargo test` 过滤器。
type TestFilter struct{}

// Name 返回 "cargo/test"，与 java/maven 这类二级命名对齐。
func (f *TestFilter) Name() string { return "cargo/test" }

// Match 只认 `cargo test ...`，拒绝 `cargo nextest run` / `cargo test-util` 等 wrapper
// ——wrapper 的输出结构不保证遵循 cargo test 的 `test result:` / `failures:` 锚点，
// 硬匹配会把不兼容输出错误压缩。
func (f *TestFilter) Match(cmd string, args []string) bool {
	if cmd != "cargo" {
		return false
	}
	if len(args) == 0 {
		return false
	}
	return args[0] == "test"
}

// testResultOkRe 匹配成功汇总：`test result: ok. 25 passed; 0 failed; ...`
var testResultOkRe = regexp.MustCompile(`^test result: ok\.`)

// testResultFailedRe 匹配失败汇总：`test result: FAILED. 26 passed; 1 failed; ...`
var testResultFailedRe = regexp.MustCompile(`^test result: FAILED\.`)

// failuresHeaderRe 匹配失败详情/列表块的起始行（cargo test 会连续打印两次：
// 第一次是详情块起头，紧跟 `---- <name> stdout ----` 小节；第二次是失败用例名列表汇总。
// 从首次出现开始截片即可覆盖两次）。
var failuresHeaderRe = regexp.MustCompile(`^failures:$`)

// findLastMatching 从后向前找首个匹配 re 的行；输出里多次出现 "running N tests" 等
// 可能造成干扰，summary 稳定在结尾，所以从后向前扫。
func findLastMatching(lines []string, re *regexp.Regexp) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if re.MatchString(strings.TrimRight(lines[i], "\r")) {
			return lines[i]
		}
	}
	return ""
}

// findFirstMatching 从前向后找首个匹配 re 的行索引；未找到返回 -1。
func findFirstMatching(lines []string, re *regexp.Regexp) int {
	for i, line := range lines {
		if re.MatchString(strings.TrimRight(line, "\r")) {
			return i
		}
	}
	return -1
}

// Apply 处理成功场景：只保留 `test result: ok.` 那一行。
// 无法 parse 出 summary 时返回原文——保守优先。
//
// 入口做 StripANSI：`CARGO_TERM_COLOR=always` 会给 `ok` / `FAILED` 加色码
// （如 `test result: \x1b[32mok\x1b[0m.`），破坏行首正则锚点。去色后匹配稳定。
func (f *TestFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	content := filter.StripANSI(original)
	lines := strings.Split(content, "\n")
	summary := findLastMatching(lines, testResultOkRe)
	if summary == "" {
		return filter.FilterOutput{Content: original, Original: original}
	}
	return filter.FilterOutput{Content: summary + "\n", Original: original}
}

// ApplyOnError 处理失败场景：保留从首个 `failures:` 行到末尾的全部内容。
// 该范围包含 panic 堆栈、assertion 细节、失败用例名汇总、最终 summary、
// "error: test failed, to rerun pass ..." 提示——LLM 诊断所需的最小完整集。
//
// 双锚点校验：既要有 `failures:` 起始，也要有 `test result: FAILED.` 汇总。
// 任一缺失都返回 nil 透传原文——避免把 `cargo test --no-run` 之类不产生
// failures 节的失败输出误压缩。
func (f *TestFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	original := input.Stdout + input.Stderr
	content := filter.StripANSI(original)
	lines := strings.Split(content, "\n")
	if findLastMatching(lines, testResultFailedRe) == "" {
		return nil
	}
	start := findFirstMatching(lines, failuresHeaderRe)
	if start < 0 {
		return nil
	}
	failBlock := strings.Join(lines[start:], "\n")
	return &filter.FilterOutput{Content: failBlock, Original: original}
}
