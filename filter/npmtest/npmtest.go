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

// Match 覆盖包管理器的测试命令 + 直接调用的 vitest：
//
//	npm test / yarn test / pnpm test / pnpm t
//	vitest run / vitest --run / vitest（以及 npx/pnpm dlx wrapper 会在各自 cmd 下透传）
//
// 排除 `npm run test:unit` 等自定义脚本——那些脚本输出不保证是标准 runner 格式。
func (f *Filter) Match(cmd string, args []string) bool {
	switch cmd {
	case "npm":
		return len(args) > 0 && args[0] == "test"
	case "yarn":
		return len(args) > 0 && args[0] == "test"
	case "pnpm":
		return len(args) > 0 && (args[0] == "test" || args[0] == "t")
	case "vitest":
		// bare `vitest`、`vitest run`、`vitest --run` 都接管
		return true
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

// --- vitest detection & compression ---

// vitestTestFilesRe 匹配 vitest summary 首行 ` Test Files  N passed (N)` / ` Test Files  1 failed (1)`。
// 行首单空格缩进是稳定特征。
var vitestTestFilesRe = regexp.MustCompile(`^ Test Files  \d+ (passed|failed)`)

// vitestFailedHeaderRe 匹配失败详情分隔符 `⎯⎯⎯⎯ Failed Tests N ⎯⎯⎯⎯`。
var vitestFailedHeaderRe = regexp.MustCompile(`^⎯+ Failed Tests \d+ ⎯+$`)

// vitestFailedFileRe 匹配失败时的文件级摘要 ` ❯ path/to/file.test.js (6 tests | 2 failed) 5ms`，
// 这一行在 stdout 里，紧跟着 `  × test > name → err` 的逐条失败快览；用它作为首选锚点
// 可以把"哪个测试文件失败、每个失败的摘要"一并纳入切片。
var vitestFailedFileRe = regexp.MustCompile(`^ ❯ \S+ \(\d+ tests? \| \d+ failed\)`)

// detectAndSliceVitest 区分成功/失败两种模式：
//   - 成功：只保留 ` Test Files` 和 ` Tests` 两行汇总；
//   - 失败：从最早的失败锚点（` ❯ file (N | M failed)` 或 `⎯ Failed Tests N ⎯`）
//     起到末尾完整保留。
//
// gw 对子进程做 stdout / stderr 分开捕获后按 "stdout + stderr" 拼接；vitest 的
// 汇总（`Test Files ...`）在 stdout，失败详情（`Failed Tests`、AssertionError、
// code frame）在 stderr，所以 concat 后 summary 会出现在 Failed-Tests 分隔符之前——
// 必须以 stdout 的 ` ❯ ` 为锚，才能同时囊括 summary、进度快览和 stderr 详情。
//
// 无法识别返回空字符串。
func detectAndSliceVitest(content string) string {
	lines := strings.Split(content, "\n")
	hasSummary := false
	for i := len(lines) - 1; i >= 0; i-- {
		if vitestTestFilesRe.MatchString(strings.TrimRight(lines[i], "\r")) {
			hasSummary = true
			break
		}
	}
	if !hasSummary {
		return ""
	}
	// 失败模式：优先用 `❯ file (... | N failed)` 锚点（stdout 里靠前的位置），
	// 缺失则退回 `Failed Tests N` 分隔符（stderr 里）。
	for i, line := range lines {
		if vitestFailedFileRe.MatchString(strings.TrimRight(line, "\r")) {
			return strings.Join(lines[i:], "\n")
		}
	}
	for i, line := range lines {
		if vitestFailedHeaderRe.MatchString(strings.TrimRight(line, "\r")) {
			return strings.Join(lines[i:], "\n")
		}
	}
	// 成功模式：保留 Test Files + Tests 两行汇总
	summaryIdx := -1
	for i, line := range lines {
		if vitestTestFilesRe.MatchString(strings.TrimRight(line, "\r")) {
			summaryIdx = i
			break
		}
	}
	end := summaryIdx + 1
	if end < len(lines) && strings.HasPrefix(strings.TrimRight(lines[end], "\r"), "      Tests ") {
		end++
	}
	return strings.Join(lines[summaryIdx:end], "\n")
}

// genericTailLines 是未识别到具体 runner 格式时的 fallback 截尾长度——
// 对齐旧 TOML 规则 `[npm.test].tail_lines = 120` 的既有行为，避免 jest/vitest
// 用户在本 filter 接管后失去 TOML 层面的基础压缩。
const genericTailLines = 120

// fallbackTail 对未识别格式应用通用尾截断：保留最后 N 行。
// 与 TOML 引擎对齐：先剥末尾 \n 再 Split，避免末尾空串被计成一行。
func fallbackTail(content string) string {
	hadTrailingNewline := strings.HasSuffix(content, "\n")
	trimmed := content
	if hadTrailingNewline {
		trimmed = content[:len(content)-1]
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) <= genericTailLines {
		return content
	}
	result := strings.Join(lines[len(lines)-genericTailLines:], "\n")
	if hadTrailingNewline && result != "" {
		result += "\n"
	}
	return result
}

// detectAndSlice 依次尝试每个 runner 的嗅探器，首个命中的结果生效。
// 顺序按 runner 特征锚点的独占性排：vitest 和 jest 的 summary 行前缀唯一，
// AVA 的 `  ─` 分隔符也唯一；无冲突。
func detectAndSlice(content string) string {
	if s := detectAndSliceVitest(content); s != "" {
		return s
	}
	if s := detectAndSliceJest(content); s != "" {
		return s
	}
	if s := detectAndSliceAVA(content); s != "" {
		return s
	}
	return ""
}

// --- jest detection & compression ---

// jestSuitesRe 匹配 jest 终态汇总 `Test Suites: 1 passed, 1 total` / `Test Suites: 1 failed, 1 total`。
// 行首无缩进、Suites 是首词。
var jestSuitesRe = regexp.MustCompile(`^Test Suites:\s+\d+ (passed|failed)`)

// jestFileResultRe 匹配文件级结果 `PASS ./file.test.js` / `FAIL src/foo.test.ts`。
// 行首无缩进、PASS/FAIL 后跟空格和路径。
var jestFileResultRe = regexp.MustCompile(`^(PASS|FAIL) \S+\.`)

// detectAndSliceJest 根据 Test Suites summary 存在与否判定 jest 输出：
//   - 失败场景（summary 里有 failed）：从首个 PASS/FAIL 文件行起到末尾，覆盖
//     测试树、● 失败块、code frame、汇总。
//   - 成功场景：只保留 Test Suites: / Tests: / Snapshots: / Time: 四行汇总。
//
// 未识别返回空字符串。
func detectAndSliceJest(content string) string {
	lines := strings.Split(content, "\n")

	// 找 summary 行
	summaryIdx := -1
	summaryFailed := false
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimRight(lines[i], "\r")
		if jestSuitesRe.MatchString(trimmed) {
			summaryIdx = i
			summaryFailed = strings.Contains(trimmed, "failed")
			break
		}
	}
	if summaryIdx < 0 {
		return ""
	}

	// 失败场景：切片从首个 PASS/FAIL 文件行到末尾（覆盖 ✓/✕ 树 + ● 详情 + 汇总）
	if summaryFailed {
		for i, line := range lines {
			if jestFileResultRe.MatchString(strings.TrimRight(line, "\r")) {
				return strings.Join(lines[i:], "\n")
			}
		}
		// 文件行缺失（罕见）：退回到 summary 起点
		return strings.Join(lines[summaryIdx:], "\n")
	}

	// 成功场景：只留 summary 起的 4 行（Test Suites / Tests / Snapshots / Time）
	end := summaryIdx + 1
	for end < len(lines) && end-summaryIdx < 4 {
		trimmed := strings.TrimRight(lines[end], "\r")
		if strings.HasPrefix(trimmed, "Tests:") ||
			strings.HasPrefix(trimmed, "Snapshots:") ||
			strings.HasPrefix(trimmed, "Time:") {
			end++
			continue
		}
		break
	}
	return strings.Join(lines[summaryIdx:end], "\n")
}

// Apply 成功场景：嗅探到已知 runner 则切片，否则落到通用尾截断。
// 入口 StripANSI：AVA 的 ✔ / ✖ 常被加色码、vitest 的 ❯ 同样带色码；
// `FORCE_COLOR=1` 或 `npm test --color` 会强制开启。去色保证嗅探器锚点稳定。
func (f *Filter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	content := filter.StripANSI(original)
	if sliced := detectAndSlice(content); sliced != "" {
		return filter.FilterOutput{Content: sliced, Original: original}
	}
	return filter.FilterOutput{Content: fallbackTail(content), Original: original}
}

// ApplyOnError 失败场景：嗅探已知 runner 优先切片，否则通用尾截断。
// 无论是否识别都返回非 nil，让上层保持压缩。
func (f *Filter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	original := input.Stdout + input.Stderr
	content := filter.StripANSI(original)
	if sliced := detectAndSlice(content); sliced != "" {
		return &filter.FilterOutput{Content: sliced, Original: original}
	}
	return &filter.FilterOutput{Content: fallbackTail(content), Original: original}
}
