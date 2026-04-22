package git

import (
	"fmt"
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&LogFilter{})
}

// LogFilter 过滤 git log 输出，转换为紧凑格式
type LogFilter struct{}

func (f *LogFilter) Name() string { return "git/log" }

func (f *LogFilter) Match(cmd string, args []string) bool {
	return cmd == "git" && len(args) > 0 && args[0] == "log"
}

func (f *LogFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	commits := parseCommits(original)

	// 锚点缺失兜底：--oneline / --pretty=format:... / --graph / 自定义 format.pretty
	// 都不会打印 "commit <hash>" + "Author:" 行，解析器产出 0 条 commit。
	// 此时直接透传原文，避免把用户输出压成空字符串（实机发现的 data loss）。
	if len(commits) == 0 {
		return filter.FilterOutput{Content: original, Original: original}
	}

	var out []string
	for _, c := range commits {
		// 短哈希 (7字符) + 主题 + 日期 + 作者
		shortHash := c.hash
		if len(shortHash) > 7 {
			shortHash = shortHash[:7]
		}
		out = append(out, fmt.Sprintf("%s %s (%s) %s", shortHash, c.subject, c.date, c.author))

		// 正文最多3行，去除 trailer
		if len(c.body) > 0 {
			bodyLines := filterTrailers(c.body)
			if len(bodyLines) > 3 {
				bodyLines = bodyLines[:3]
			}
			for _, bl := range bodyLines {
				out = append(out, "  "+bl)
			}
		}
		out = append(out, "")
	}

	content := strings.Join(out, "\n")
	return filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

func (f *LogFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	return nil
}

type commit struct {
	hash    string
	author  string
	date    string
	subject string
	body    []string
}

// parseCommits 从 git log 默认格式解析提交
func parseCommits(text string) []commit {
	lines := strings.Split(text, "\n")
	var commits []commit
	var current *commit

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "commit ") {
			if current != nil {
				current.body = trimEmptyLines(current.body)
				commits = append(commits, *current)
			}
			current = &commit{hash: strings.TrimPrefix(line, "commit ")}
		} else if current != nil {
			if strings.HasPrefix(line, "Author: ") {
				// 提取作者名（去掉邮箱部分）
				authorFull := strings.TrimPrefix(line, "Author: ")
				if idx := strings.Index(authorFull, " <"); idx != -1 {
					current.author = authorFull[:idx]
				} else {
					current.author = authorFull
				}
			} else if strings.HasPrefix(line, "Date:   ") {
				current.date = strings.TrimSpace(strings.TrimPrefix(line, "Date:"))
			} else if strings.HasPrefix(line, "    ") {
				content := strings.TrimPrefix(line, "    ")
				if current.subject == "" {
					current.subject = content
				} else {
					current.body = append(current.body, content)
				}
			}
		}
	}
	if current != nil {
		current.body = trimEmptyLines(current.body)
		commits = append(commits, *current)
	}
	return commits
}

// filterTrailers 去除 Signed-off-by 和 Co-authored-by 行
func filterTrailers(lines []string) []string {
	var result []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "Signed-off-by:") || strings.HasPrefix(trimmed, "Co-authored-by:") {
			continue
		}
		result = append(result, l)
	}
	return trimEmptyLines(result)
}

// trimEmptyLines 去除首尾空行
func trimEmptyLines(lines []string) []string {
	// 去头部空行
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	// 去尾部空行
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
