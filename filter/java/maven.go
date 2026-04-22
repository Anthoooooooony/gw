package java

import (
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&MavenFilter{})
}

// MavenFilter 过滤 Maven 构建输出，使用状态机压缩噪音
type MavenFilter struct{}

func (f *MavenFilter) Name() string { return "java/maven" }

// actionType 表示对一行的处理动作
type actionType int

const (
	ActionDrop      actionType = iota // 丢弃
	ActionKeep                        // 保留
	ActionKeepError                   // 保留 + 去重
)

// longRunningGoals 是可能导致长驻进程的 Maven goal
var longRunningGoals = []string{
	"spring-boot:run",
	"jetty:run",
	"tomcat7:run",
	"liberty:run",
	"quarkus:dev",
	"exec:java",
}

func (f *MavenFilter) Match(cmd string, args []string) bool {
	if cmd != "mvn" {
		return false
	}
	// 排除长驻进程的 goal
	for _, arg := range args {
		for _, goal := range longRunningGoals {
			if arg == goal {
				return false
			}
		}
	}
	return true
}

func (f *MavenFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout + input.Stderr
	content := processMavenOutput(original, true)
	return filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

func (f *MavenFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	original := input.Stdout + input.Stderr
	content := processMavenOutput(original, false)
	return &filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

// processMavenOutput 用状态机处理 Maven 输出
func processMavenOutput(output string, successMode bool) string {
	lines := strings.Split(output, "\n")
	state := StateInit
	seenErrors := make(map[string]bool)
	var result []string

	for _, line := range lines {
		lc := classifyLine(line)
		state = nextState(state, lc)
		action := decideAction(state, lc, successMode)

		switch action {
		case ActionDrop:
			continue
		case ActionKeep:
			result = append(result, stripPrefix(line))
		case ActionKeepError:
			stripped := stripPrefix(line)
			key := extractErrorKey(stripped)
			if key != "" {
				if seenErrors[key] {
					continue
				}
				seenErrors[key] = true
			}
			result = append(result, stripped)
		}
	}

	return collapseBlankLines(result)
}

// decideAction 根据状态、行分类、模式决定处理动作
func decideAction(state MavenState, lc MavenLineClass, successMode bool) actionType {
	// 全局丢弃（任何状态）
	switch lc {
	case LineDiscovery, LineSeparator, LineEmpty, LineFinishedAt,
		LineTransfer, LinePomWarning, LineCompilerWarning,
		LineProcessNoise, LineHelpSuggestion:
		return ActionDrop
	}

	// 按状态处理
	switch state {
	case StateInit, StateDiscovery, StateWarning:
		return ActionDrop

	case StateModuleBuild:
		// Building xxx 行 — Reactor Summary 已经包含此信息
		return ActionDrop

	case StateMojo:
		// --- plugin:ver:goal --- 是噪音
		return ActionDrop

	case StatePluginOutput:
		// 测试摘要在任何模式下都保留
		if lc == LineTestSummary {
			return ActionKeep
		}
		if successMode {
			return ActionDrop
		}
		// 失败模式
		if lc == LineError {
			return ActionKeepError
		}
		if lc == LineStackTrace {
			return ActionKeep
		}
		return ActionDrop

	case StateTestOutput:
		if lc == LineTestHeader {
			return ActionDrop
		}
		if lc == LineTestSummary {
			return ActionKeep
		}
		if lc == LineTestRunning {
			if successMode {
				return ActionDrop
			}
			return ActionKeep
		}
		if lc == LineError {
			return ActionKeepError
		}
		if lc == LineStackTrace {
			return ActionKeep
		}
		// 与 stream 版本对齐：测试输出状态下非诊断行（应用日志、时间戳行、
		// Spring ApplicationContext 诊断对象序列化等）在失败模式下也必须 Drop，
		// 否则 spring-boot 类项目一次集成测试失败的 ApplicationContext 加载输出
		// 能把 300KB 原始压到只剩几 KB 的压缩优势吃掉（issue #42：batch 4.6% vs stream 73%）。
		return ActionDrop

	case StateReactor:
		if lc == LineReactorEntry {
			return ActionKeep
		}
		return ActionDrop

	case StateResult:
		if lc == LineBuildResult {
			return ActionKeep
		}
		return ActionDrop

	case StateStats:
		if lc == LineStats {
			return ActionKeep
		}
		return ActionDrop

	case StateErrorReport:
		if lc == LineError {
			return ActionKeepError
		}
		if lc == LineStackTrace {
			return ActionKeep
		}
		return ActionDrop
	}

	return ActionDrop
}

// stripPrefix 去除 [INFO]/[ERROR]/[WARNING] 前缀
func stripPrefix(line string) string {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range []string{"[INFO] ", "[ERROR] ", "[WARNING] "} {
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimPrefix(trimmed, prefix)
		}
	}
	// 处理仅有标签无空格的情况 (如 "[INFO]")
	for _, tag := range []string{"[INFO]", "[ERROR]", "[WARNING]"} {
		if trimmed == tag {
			return ""
		}
	}
	return trimmed
}

// extractErrorKey 提取错误去重键
func extractErrorKey(errLine string) string {
	if strings.Contains(errLine, "Unresolved reference") {
		parts := strings.SplitN(errLine, "Unresolved reference", 2)
		if len(parts) == 2 {
			return "unresolved:" + strings.TrimSpace(parts[1])
		}
	}
	if strings.Contains(errLine, "Type mismatch") {
		parts := strings.SplitN(errLine, "Type mismatch", 2)
		if len(parts) == 2 {
			return "type_mismatch:" + strings.TrimSpace(parts[1])
		}
	}
	if strings.Contains(errLine, "Cannot access class") {
		parts := strings.SplitN(errLine, "Cannot access class", 2)
		if len(parts) == 2 {
			return "access:" + strings.TrimSpace(parts[1])
		}
	}
	return ""
}

// collapseBlankLines 合并连续空行，并去掉首尾空行
func collapseBlankLines(lines []string) string {
	var result []string
	prevBlank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if prevBlank {
				continue
			}
			prevBlank = true
		} else {
			prevBlank = false
		}
		result = append(result, line)
	}

	// 去掉首尾空行
	for len(result) > 0 && strings.TrimSpace(result[0]) == "" {
		result = result[1:]
	}
	for len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
		result = result[:len(result)-1]
	}

	return strings.Join(result, "\n")
}

// NewStreamInstance 创建流式处理器实例
func (f *MavenFilter) NewStreamInstance() filter.StreamProcessor {
	return &mavenStreamProcessor{
		state:      StateInit,
		seenErrors: newBoundedDedupSet(mavenDedupCap),
	}
}

// mavenDedupCap 是 stream 版本 seenErrors 的硬上限，防止长构建 OOM。
// 批量版本（processMavenOutput）一次性处理固定 buffer 不受此影响。
const mavenDedupCap = 10000

type mavenStreamProcessor struct {
	state      MavenState
	seenErrors *boundedDedupSet
	buffer     []string
}

func (p *mavenStreamProcessor) ProcessLine(line string) (filter.StreamAction, string) {
	lc := classifyLine(line)
	p.state = nextState(p.state, lc)

	// 全局丢弃
	switch lc {
	case LineDiscovery, LineSeparator, LineEmpty, LineFinishedAt,
		LineTransfer, LinePomWarning, LineCompilerWarning,
		LineProcessNoise, LineHelpSuggestion:
		return filter.StreamDrop, ""
	}

	switch p.state {
	case StateInit, StateDiscovery, StateWarning:
		return filter.StreamDrop, ""

	case StateModuleBuild:
		return filter.StreamDrop, ""

	case StateMojo:
		p.buffer = nil
		return filter.StreamDrop, ""

	case StatePluginOutput:
		if lc == LineError {
			stripped := stripPrefix(line)
			key := extractErrorKey(stripped)
			if key != "" {
				if p.seenErrors.Has(key) {
					return filter.StreamDrop, ""
				}
				p.seenErrors.Add(key)
			}
			return filter.StreamEmit, stripped
		}
		if lc == LineStackTrace {
			return filter.StreamEmit, stripPrefix(line)
		}
		// 缓冲其他行，最多 10 行
		if len(p.buffer) < 10 {
			p.buffer = append(p.buffer, stripPrefix(line))
		}
		return filter.StreamDrop, ""

	case StateTestOutput:
		p.buffer = nil // 离开 PluginOutput，清空旧缓冲
		if lc == LineTestHeader || lc == LineTestRunning {
			return filter.StreamDrop, ""
		}
		if lc == LineTestSummary {
			return filter.StreamEmit, stripPrefix(line)
		}
		if lc == LineError {
			stripped := stripPrefix(line)
			key := extractErrorKey(stripped)
			if key != "" {
				if p.seenErrors.Has(key) {
					return filter.StreamDrop, ""
				}
				p.seenErrors.Add(key)
			}
			return filter.StreamEmit, stripped
		}
		if lc == LineStackTrace {
			return filter.StreamEmit, stripPrefix(line)
		}
		return filter.StreamDrop, ""

	case StateReactor:
		p.buffer = nil // 进入 Reactor，清空旧缓冲
		if lc == LineReactorEntry {
			return filter.StreamEmit, stripPrefix(line)
		}
		return filter.StreamDrop, ""

	case StateResult:
		if lc == LineBuildResult {
			return filter.StreamEmit, stripPrefix(line)
		}
		return filter.StreamDrop, ""

	case StateStats:
		if lc == LineStats {
			return filter.StreamEmit, stripPrefix(line)
		}
		return filter.StreamDrop, ""

	case StateErrorReport:
		if lc == LineError {
			stripped := stripPrefix(line)
			key := extractErrorKey(stripped)
			if key != "" {
				if p.seenErrors.Has(key) {
					return filter.StreamDrop, ""
				}
				p.seenErrors.Add(key)
			}
			return filter.StreamEmit, stripped
		}
		if lc == LineStackTrace {
			return filter.StreamEmit, stripPrefix(line)
		}
		return filter.StreamDrop, ""
	}

	return filter.StreamDrop, ""
}

func (p *mavenStreamProcessor) Flush(exitCode int) []string {
	if exitCode != 0 && len(p.buffer) > 0 {
		return p.buffer
	}
	return nil
}
