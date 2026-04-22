package toml

import (
	"embed"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(LoadEngine())
}

//go:embed rules/*.toml
var builtinRules embed.FS

// Rule 定义一条 TOML 声明式过滤规则。
//
// 设计哲学（v2）：**只做无损变换**。DSL 故意不提供 strip_lines / keep_lines /
// on_error 这类基于正则的行级裁剪——词法匹配无法区分"真噪音"和"用户恰好需要的
// 那一行"，长期会产生误删信任危机。想要激进压缩（pytest 只留 failures、
// vitest 生成 "PASS/FAIL" 摘要等）请写专属 Go filter，按命令语义 parse 后生成摘要。
//
// 保留的字段都是**语义无关**的安全变换：
//   - StripAnsi：ANSI 转义序列纯视觉噪音
//   - HeadLines/TailLines/MaxLines：纯长度兜底（截首/截尾/硬上限）
//   - OnEmpty：输出被截断到空时的友好提示
type Rule struct {
	Match     string `toml:"match"`      // 命令前缀匹配
	StripAnsi bool   `toml:"strip_ansi"` // 移除 ANSI 转义码（无损）
	MaxLines  int    `toml:"max_lines"`  // 截断到 N 行
	HeadLines int    `toml:"head_lines"` // 保留前 N 行
	TailLines int    `toml:"tail_lines"` // 保留后 N 行
	OnEmpty   string `toml:"on_empty"`   // 输出为空时的替代消息
}

// TomlFilter 基于 TOML 规则的声明式过滤器（无状态：所有字段一次加载后只读）
type TomlFilter struct {
	Rules  []Rule
	Loaded []LoadedRule // 带来源的完整规则信息（可能为空，用于 filters list）
}

// LoadEngine 使用三级加载器（builtin + user + project）构造过滤器实例。
// 即使加载过程遇到错误也会返回可用的空实例，避免影响主流程。
func LoadEngine() *TomlFilter {
	loaded := LoadAllRules()
	rules := make([]Rule, 0, len(loaded))
	for _, l := range loaded {
		rules = append(rules, l.Rule)
	}
	return &TomlFilter{Rules: rules, Loaded: loaded}
}

func (f *TomlFilter) Name() string { return "toml" }

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

func (f *TomlFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	return nil
}

// findRule 查找最长前缀匹配的规则
func (f *TomlFilter) findRule(fullCmd string) *Rule {
	var best *Rule
	bestLen := 0
	for i := range f.Rules {
		r := &f.Rules[i]
		if strings.HasPrefix(fullCmd, r.Match) && len(r.Match) > bestLen {
			best = r
			bestLen = len(r.Match)
		}
	}
	return best
}

// applyRule 按管道顺序应用无损变换：strip_ansi → head_lines → tail_lines → max_lines → on_empty
func applyRule(rule *Rule, content string) string {
	if rule.StripAnsi {
		content = filter.StripANSI(content)
	}

	lines := strings.Split(content, "\n")

	if rule.HeadLines > 0 && len(lines) > rule.HeadLines {
		lines = lines[:rule.HeadLines]
	}
	if rule.TailLines > 0 && len(lines) > rule.TailLines {
		lines = lines[len(lines)-rule.TailLines:]
	}
	if rule.MaxLines > 0 && len(lines) > rule.MaxLines {
		lines = lines[:rule.MaxLines]
	}

	result := strings.Join(lines, "\n")
	if rule.OnEmpty != "" && strings.TrimSpace(result) == "" {
		return rule.OnEmpty
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

// LoadBuiltinRules 加载嵌入的 TOML 规则文件
func LoadBuiltinRules() (*TomlFilter, error) {
	entries, err := builtinRules.ReadDir("rules")
	if err != nil {
		return nil, err
	}

	f := &TomlFilter{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		data, err := builtinRules.ReadFile("rules/" + entry.Name())
		if err != nil {
			continue
		}
		var ruleMap map[string]map[string]Rule
		if _, err := toml.Decode(string(data), &ruleMap); err != nil {
			continue
		}
		for _, group := range ruleMap {
			for _, rule := range group {
				f.Rules = append(f.Rules, rule)
			}
		}
	}
	return f, nil
}
