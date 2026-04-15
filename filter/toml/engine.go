package toml

import (
	"embed"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/gw-cli/gw/filter"
)

//go:embed rules/*.toml
var builtinRules embed.FS

// Rule 定义一条 TOML 声明式过滤规则
type Rule struct {
	Match      string   `toml:"match"`       // 命令前缀匹配
	StripAnsi  bool     `toml:"strip_ansi"`  // 移除 ANSI 转义码
	MaxLines   int      `toml:"max_lines"`   // 截断到 N 行
	HeadLines  int      `toml:"head_lines"`  // 保留前 N 行
	TailLines  int      `toml:"tail_lines"`  // 保留后 N 行
	StripLines []string `toml:"strip_lines"` // 按正则移除行
	KeepLines  []string `toml:"keep_lines"`  // 仅保留包含指定字符串的行
	OnEmpty    string   `toml:"on_empty"`    // 输出为空时的替代消息
}

// TomlFilter 基于 TOML 规则的声明式过滤器
type TomlFilter struct {
	Rules []Rule
}

// ansiRegex 匹配 ANSI 转义序列
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func (f *TomlFilter) Match(cmd string, args []string) bool {
	fullCmd := buildFullCmd(cmd, args)
	return f.findRule(fullCmd) != nil
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

// applyRule 按管道顺序应用规则: strip_ansi → strip_lines → keep_lines → head_lines → tail_lines → max_lines → on_empty
func applyRule(rule *Rule, content string) string {
	// strip_ansi
	if rule.StripAnsi {
		content = ansiRegex.ReplaceAllString(content, "")
	}

	lines := strings.Split(content, "\n")

	// strip_lines: 按正则移除匹配的行
	if len(rule.StripLines) > 0 {
		var patterns []*regexp.Regexp
		for _, p := range rule.StripLines {
			if re, err := regexp.Compile(p); err == nil {
				patterns = append(patterns, re)
			}
		}
		var filtered []string
		for _, line := range lines {
			matched := false
			for _, re := range patterns {
				if re.MatchString(line) {
					matched = true
					break
				}
			}
			if !matched {
				filtered = append(filtered, line)
			}
		}
		lines = filtered
	}

	// keep_lines: 仅保留包含指定字符串的行
	if len(rule.KeepLines) > 0 {
		var filtered []string
		for _, line := range lines {
			for _, kw := range rule.KeepLines {
				if strings.Contains(line, kw) {
					filtered = append(filtered, line)
					break
				}
			}
		}
		lines = filtered
	}

	// head_lines: 保留前 N 行
	if rule.HeadLines > 0 && len(lines) > rule.HeadLines {
		lines = lines[:rule.HeadLines]
	}

	// tail_lines: 保留后 N 行
	if rule.TailLines > 0 && len(lines) > rule.TailLines {
		lines = lines[len(lines)-rule.TailLines:]
	}

	// max_lines: 截断到 N 行
	if rule.MaxLines > 0 && len(lines) > rule.MaxLines {
		lines = lines[:rule.MaxLines]
	}

	result := strings.Join(lines, "\n")

	// on_empty
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
