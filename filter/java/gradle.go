package java

import (
	"path/filepath"
	"strings"

	"github.com/gw-cli/gw/filter"
)

// GradleFilter 过滤 Gradle 构建输出，压缩任务进度和守护进程启动信息
type GradleFilter struct{}

func (f *GradleFilter) Name() string { return "java/gradle" }

func (f *GradleFilter) Match(cmd string, args []string) bool {
	base := filepath.Base(cmd)
	return base == "gradle" || base == "gradlew"
}

func (f *GradleFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout + input.Stderr
	lines := strings.Split(original, "\n")

	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 去除 Task 进度行
		if strings.HasPrefix(trimmed, "> Task :") {
			continue
		}

		// 去除 Starting Daemon 行
		if strings.HasPrefix(trimmed, "Starting a Gradle Daemon") {
			continue
		}

		filtered = append(filtered, line)
	}

	content := collapseBlankLines(filtered)
	return filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

func (f *GradleFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	original := input.Stdout + input.Stderr
	lines := strings.Split(original, "\n")

	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 去除普通 Task 进度行（保留 FAILED 的任务）
		if strings.HasPrefix(trimmed, "> Task :") && !strings.HasSuffix(trimmed, "FAILED") {
			continue
		}

		// 去除 Starting Daemon 行
		if strings.HasPrefix(trimmed, "Starting a Gradle Daemon") {
			continue
		}

		// 去除 Try 建议行
		if strings.HasPrefix(trimmed, "> Run with --") {
			continue
		}

		// 去除报告文件链接
		if strings.Contains(trimmed, "See the report at:") {
			continue
		}

		filtered = append(filtered, line)
	}

	content := collapseBlankLines(filtered)
	return &filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}
