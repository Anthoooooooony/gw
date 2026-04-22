package toml

import (
	"embed"
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(LoadEngine())
}

//go:embed rules/*.toml
var builtinRules embed.FS

// Rule 定义一条 TOML 声明式过滤规则。
//
// 设计哲学（v2）：**只做无损变换**。DSL 故意不提供 strip_lines / keep_lines
// 这类基于正则的行级裁剪——词法匹配无法区分"真噪音"和"用户恰好需要的那一行"，
// 长期会产生误删信任危机。想要激进压缩（pytest 只留 failures、vitest 生成
// "PASS/FAIL" 摘要等）请写专属 Go filter，按命令语义 parse 后生成摘要。
//
// 保留的字段都是**语义无关**的安全变换：
//   - StripAnsi：ANSI 转义序列纯视觉噪音
//   - HeadLines/TailLines/MaxLines：纯长度兜底（截首/截尾/硬上限）
//   - OnEmpty：输出被截断到空时的友好提示
//   - OnError：失败场景独立参数（issue #43）——字段集与主规则完全一致，
//     仍严守"仅无损变换"。成功/失败的 tail_lines 往往差一个数量级，
//     单一参数盖不住两种场景。
type Rule struct {
	Match     string       `toml:"match"`      // 命令前缀匹配
	StripAnsi bool         `toml:"strip_ansi"` // 移除 ANSI 转义码（无损）
	MaxLines  int          `toml:"max_lines"`  // 截断到 N 行
	HeadLines int          `toml:"head_lines"` // 保留前 N 行
	TailLines int          `toml:"tail_lines"` // 保留后 N 行
	OnEmpty   string       `toml:"on_empty"`   // 输出为空时的替代消息
	OnError   *OnErrorRule `toml:"on_error"`   // 失败场景的独立参数；nil 时 ApplyOnError 透传
}

// OnErrorRule 是失败场景的独立参数表。字段集与 Rule 无损变换部分一致，
// 不含 match（沿用父规则）和 on_error（禁止嵌套）。
type OnErrorRule struct {
	StripAnsi bool   `toml:"strip_ansi"`
	MaxLines  int    `toml:"max_lines"`
	HeadLines int    `toml:"head_lines"`
	TailLines int    `toml:"tail_lines"`
	OnEmpty   string `toml:"on_empty"`
}

// TomlFilter 基于 TOML 规则的声明式过滤器（无状态：所有字段一次加载后只读）
type TomlFilter struct {
	Loaded []LoadedRule // 带来源的完整规则信息，承载全部 filter 行为
}

// LoadEngine 使用三级加载器（builtin + user + project）构造过滤器实例。
// 即使加载过程遇到错误也会返回可用的空实例，避免影响主流程。
func LoadEngine() *TomlFilter {
	return &TomlFilter{Loaded: LoadAllRules()}
}

func (f *TomlFilter) Name() string { return "toml" }

// IsFallback 声明 TomlFilter 是兜底过滤器：专属 Go filter 优先匹配。
// 这样 filter/all 的导入顺序不再隐含优先级不变式。
func (f *TomlFilter) IsFallback() bool { return true }

// Describe 产出 `gw filters list` 所需的每条 rule 一行展开。
func (f *TomlFilter) Describe() []filter.FilterRow {
	rows := make([]filter.FilterRow, 0, len(f.Loaded))
	for _, lr := range f.Loaded {
		rows = append(rows, filter.FilterRow{
			Name:   lr.ID,
			Type:   "toml",
			Source: lr.Source,
			Match:  lr.Rule.Match,
		})
	}
	return rows
}

// Subname 实现 filter.SubnameResolver：返回本次 (cmd, args) 匹配到的 rule.Match，未匹配返回空。
// 纯函数：不依赖 / 不修改 filter 实例状态。
func (f *TomlFilter) Subname(cmd string, args []string) string {
	if rule := f.findRule(buildFullCmd(cmd, args)); rule != nil {
		return rule.Match
	}
	return ""
}

func (f *TomlFilter) Match(cmd string, args []string) bool {
	return f.findRule(buildFullCmd(cmd, args)) != nil
}

func (f *TomlFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	fullCmd := buildFullCmd(input.Cmd, input.Args)
	rule := f.findRule(fullCmd)
	if rule == nil {
		return filter.FilterOutput{Content: input.Stdout, Original: input.Stdout}
	}

	content := applyRule(rule, input.Stdout)
	return filter.FilterOutput{Content: content, Original: input.Stdout}
}

// ApplyOnError 对命中规则且配置了 [section.name.on_error] 子表的命令，
// 按 OnError 参数做截断/清洗；没配则返回 nil 表示透传原始输出。
func (f *TomlFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	fullCmd := buildFullCmd(input.Cmd, input.Args)
	rule := f.findRule(fullCmd)
	if rule == nil || rule.OnError == nil {
		return nil
	}
	original := input.Stdout + input.Stderr
	content := applyOnErrorRule(rule.OnError, original)
	return &filter.FilterOutput{Content: content, Original: original}
}

// findRule 查找最长前缀匹配的规则
func (f *TomlFilter) findRule(fullCmd string) *Rule {
	var best *Rule
	bestLen := 0
	for i := range f.Loaded {
		r := &f.Loaded[i].Rule
		if strings.HasPrefix(fullCmd, r.Match) && len(r.Match) > bestLen {
			best = r
			bestLen = len(r.Match)
		}
	}
	return best
}

// applyRule 按管道顺序应用无损变换：strip_ansi → head_lines → tail_lines → max_lines → on_empty
func applyRule(rule *Rule, content string) string {
	return applyLossless(rule.StripAnsi, rule.HeadLines, rule.TailLines, rule.MaxLines, rule.OnEmpty, content)
}

// applyOnErrorRule 用 OnErrorRule 的参数走同一套无损管道。
func applyOnErrorRule(rule *OnErrorRule, content string) string {
	return applyLossless(rule.StripAnsi, rule.HeadLines, rule.TailLines, rule.MaxLines, rule.OnEmpty, content)
}

func applyLossless(stripAnsi bool, head, tail, maxL int, onEmpty, content string) string {
	if stripAnsi {
		content = filter.StripANSI(content)
	}

	lines := strings.Split(content, "\n")

	if head > 0 && len(lines) > head {
		lines = lines[:head]
	}
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	if maxL > 0 && len(lines) > maxL {
		lines = lines[:maxL]
	}

	result := strings.Join(lines, "\n")
	if onEmpty != "" && strings.TrimSpace(result) == "" {
		return onEmpty
	}
	return result
}

// buildFullCmd 将命令和参数拼成完整命令字符串
func buildFullCmd(cmd string, args []string) string {
	if len(args) == 0 {
		return cmd
	}
	return cmd + " " + strings.Join(args, " ")
}
