package cargo

import (
	"regexp"
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&BuildFilter{})
}

// BuildFilter 处理 `cargo build|check|clippy` 三个语义同构的子命令。
// 它们的输出结构共享相同锚点：成功以 `Finished ...profile... in Ns` 结尾，
// 失败末尾打印 `error: could not compile \`crate\` (...) due to N previous error`。
type BuildFilter struct{}

// Name 返回 "cargo/build"。对于 check/clippy 通过 Subname 区分展示。
func (f *BuildFilter) Name() string { return "cargo/build" }

// cargoBuildSubcmds 是本 filter 接管的 cargo 子命令集合。
var cargoBuildSubcmds = map[string]bool{
	"build":  true,
	"check":  true,
	"clippy": true,
}

// Match 只认 `cargo {build|check|clippy} ...`，排除 nextest 等 wrapper。
func (f *BuildFilter) Match(cmd string, args []string) bool {
	if cmd != "cargo" || len(args) == 0 {
		return false
	}
	return cargoBuildSubcmds[args[0]]
}

// Subname 返回实际子命令（build / check / clippy），让 FilterUsed 显示 "cargo/build/check" 之类。
func (f *BuildFilter) Subname(cmd string, args []string) string {
	if cmd != "cargo" || len(args) == 0 {
		return ""
	}
	if cargoBuildSubcmds[args[0]] {
		return args[0]
	}
	return ""
}

// finishedRe 匹配成功结尾行：`    Finished \`dev\` profile [unoptimized + debuginfo] target(s) in 8.83s`
// 以及 `cargo test` 前导的 `    Finished \`test\` profile ...` （但本 filter 不匹配 cargo test）。
var finishedRe = regexp.MustCompile(`^\s*Finished .*target\(s\) in [\d.]+s`)

// couldNotCompileRe 匹配失败结尾：`error: could not compile \`grep-matcher\` (lib) due to 1 previous error`
var couldNotCompileRe = regexp.MustCompile(`^error: could not compile`)

// errorLineRe 匹配首个 cargo 诊断行：`error:` / `error[E0308]:` / `warning:` 不匹配（只关心 error）。
var errorLineRe = regexp.MustCompile(`^error(\[E\d+\])?:`)

// Apply 成功场景：只保留 Finished 行。未找到返回原文。
func (f *BuildFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	content := input.Stdout
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if finishedRe.MatchString(strings.TrimRight(lines[i], "\r")) {
			return filter.FilterOutput{Content: strings.TrimSpace(lines[i]) + "\n", Original: content}
		}
	}
	return filter.FilterOutput{Content: content, Original: content}
}

// ApplyOnError 失败场景：保留从首个 `error:` 行到末尾。
// 双锚点校验：既要存在首个 error 行，也要存在 `error: could not compile` 总结行。
// 任一缺失返回 nil，避免误压缩 `cargo build --timings` 等非标形态。
func (f *BuildFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	content := input.Stdout + input.Stderr
	lines := strings.Split(content, "\n")

	// 总结行（could not compile）必须存在
	sawSummary := false
	for i := len(lines) - 1; i >= 0; i-- {
		if couldNotCompileRe.MatchString(strings.TrimRight(lines[i], "\r")) {
			sawSummary = true
			break
		}
	}
	if !sawSummary {
		return nil
	}

	// 首个 error 行作为切片起点
	start := -1
	for i, line := range lines {
		if errorLineRe.MatchString(strings.TrimRight(line, "\r")) {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	block := strings.Join(lines[start:], "\n")
	return &filter.FilterOutput{Content: block, Original: content}
}
