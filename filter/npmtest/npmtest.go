// Package npmtest 处理 `npm test` / `yarn test` / `pnpm test` 的输出。
//
// 设计：同一个 `npm test` 命令可能运行 AVA / jest / vitest / mocha / tap 等不同
// test runner，每个有独特的 summary 锚点。Filter 按 runner 做格式嗅探：
//
//	AVA：   `  N tests? (passed|failed)` + `  ─` 分隔线
//	jest：  `Tests:       N failed, M passed, ...`
//	vitest：` Test Files  N passed` / ` Tests       N passed`
//	mocha： `  N passing ...` / `  N failing ...`
//
// 当前实现只覆盖 AVA（有 fixture）。其他 runner 的分支预留接口，
// 命中则走原有锚点切片，未命中返回原文（保守不盲删）。
package npmtest

import (
	"regexp"
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&Filter{})
}

// Filter 统一处理 npm/yarn/pnpm test。
type Filter struct{}

// Name 返回 "npm/test"。
func (f *Filter) Name() string { return "npm/test" }

// Match 覆盖三种包管理器：`npm test [...]`、`yarn test [...]`、`pnpm test [...]`、`pnpm t`。
// 排除 `npm run test:unit` 等自定义脚本——那些脚本输出不一定是标准 test runner 格式。
func (f *Filter) Match(cmd string, args []string) bool {
	switch cmd {
	case "npm":
		return len(args) > 0 && args[0] == "test"
	case "yarn":
		return len(args) > 0 && args[0] == "test"
	case "pnpm":
		return len(args) > 0 && (args[0] == "test" || args[0] == "t")
	}
	return false
}

// Subname 让 FilterUsed 显示诊断用的 "npm/test/<pm>" 格式。
func (f *Filter) Subname(cmd string, args []string) string { return cmd }

// --- AVA detection & compression ---

// avaSummaryRe 匹配 AVA 终态行：`  32 tests passed` / `  1 test failed` /
// `  2 tests failed` / `  5 tests passed with 1 known failure`。
// 注意行首两空格缩进是 AVA 稳定输出的一部分。
var avaSummaryRe = regexp.MustCompile(`^  \d+ tests? (passed|failed)`)

// avaSeparatorRe 匹配 AVA 的块分隔线 `  ─`（U+2500 BOX DRAWINGS LIGHT HORIZONTAL）。
var avaSeparatorRe = regexp.MustCompile(`^  ─\s*$`)

// detectAndSliceAVA 检测 AVA 格式并返回从首个 `  ─` 到末尾的切片；
// 非 AVA 返回空字符串。
func detectAndSliceAVA(content string) string {
	lines := strings.Split(content, "\n")
	// 必须存在 summary 行，否则不是完整的 AVA 输出
	hasSummary := false
	for i := len(lines) - 1; i >= 0; i-- {
		if avaSummaryRe.MatchString(strings.TrimRight(lines[i], "\r")) {
			hasSummary = true
			break
		}
	}
	if !hasSummary {
		return ""
	}
	// 找首个分隔线
	for i, line := range lines {
		if avaSeparatorRe.MatchString(strings.TrimRight(line, "\r")) {
			return strings.Join(lines[i:], "\n")
		}
	}
	// summary 存在但分隔线缺失（非典型 AVA）→ 不切，原文透传
	return ""
}

// genericTailLines 是未识别到具体 runner 格式时的 fallback 截尾长度——
// 对齐旧 TOML 规则 `[npm.test].tail_lines = 120` 的既有行为，避免 jest/vitest
// 用户在本 filter 接管后失去 TOML 层面的基础压缩。
const genericTailLines = 120

// fallbackTail 对未识别格式应用通用尾截断：保留最后 N 行。
func fallbackTail(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= genericTailLines {
		return content
	}
	return strings.Join(lines[len(lines)-genericTailLines:], "\n")
}

// Apply 成功场景：嗅探到 AVA 则切片，否则落到通用尾截断（对齐旧 TOML 行为）。
func (f *Filter) Apply(input filter.FilterInput) filter.FilterOutput {
	content := input.Stdout
	if sliced := detectAndSliceAVA(content); sliced != "" {
		return filter.FilterOutput{Content: sliced, Original: content}
	}
	return filter.FilterOutput{Content: fallbackTail(content), Original: content}
}

// ApplyOnError 失败场景：AVA 与成功场景共用相同分隔线锚点——切片一样做；
// 非 AVA 格式做通用尾截断。无论是否匹配到 runner 都返回非 nil，让上层保持压缩。
func (f *Filter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	content := input.Stdout + input.Stderr
	if sliced := detectAndSliceAVA(content); sliced != "" {
		return &filter.FilterOutput{Content: sliced, Original: content}
	}
	return &filter.FilterOutput{Content: fallbackTail(content), Original: content}
}
