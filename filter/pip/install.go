// Package pip 是 pip 子命令的语义感知过滤器集合。
//
// 当前仅覆盖 `pip install`，锚点策略对齐 filter/pytest 范本。
package pip

import (
	"regexp"
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&InstallFilter{})
}

// InstallFilter 是无状态的 `pip install` 过滤器，同时覆盖 `pip3 install`
// 和 `python -m pip install`。
type InstallFilter struct{}

// Name 返回 "pip/install"。
func (f *InstallFilter) Name() string { return "pip/install" }

// Match 识别三种 pip install 调用：
//
//	pip install <pkg>
//	pip3 install <pkg>
//	python[3] -m pip install <pkg>
//
// 不识别 wrapper（如 `uv pip install`）——输出结构差异太大，硬匹配会误压缩。
func (f *InstallFilter) Match(cmd string, args []string) bool {
	switch cmd {
	case "pip", "pip3":
		return len(args) > 0 && args[0] == "install"
	case "python", "python3":
		// `-m pip install` 或 `-mpip install ...`
		for i, a := range args {
			if a == "-m" && i+1 < len(args) && args[i+1] == "pip" {
				// 还需要有 install 子命令
				for _, rest := range args[i+2:] {
					if rest == "install" {
						return true
					}
				}
				return false
			}
			if a == "-mpip" {
				for _, rest := range args[i+1:] {
					if rest == "install" {
						return true
					}
				}
				return false
			}
		}
	}
	return false
}

// Subname 展示 pip invocation 形式，便于诊断。
func (f *InstallFilter) Subname(cmd string, args []string) string {
	switch cmd {
	case "pip", "pip3":
		return ""
	case "python", "python3":
		return cmd + " -m pip install"
	}
	return ""
}

// successRe 匹配 `Successfully installed pkg-1.2.3 other-4.5.6`。
// 这是 pip install 成功的稳定终态锚点，稳定在末尾。
var successRe = regexp.MustCompile(`^Successfully installed `)

// errorRe 匹配 `ERROR: ...` 开头，pip 失败时的标准诊断前缀。
var errorRe = regexp.MustCompile(`^ERROR: `)

// Apply 成功场景：只保留 `Successfully installed` 行。
// 找不到时透传原文（用户可能跑的是 `pip install -e .` 且被插件改过输出，
// 保守不盲删）。
func (f *InstallFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	content := input.Stdout
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if successRe.MatchString(strings.TrimRight(lines[i], "\r")) {
			return filter.FilterOutput{Content: lines[i] + "\n", Original: content}
		}
	}
	return filter.FilterOutput{Content: content, Original: content}
}

// ApplyOnError 失败场景：保留所有 `ERROR: ` 行（pip 把每条失败原因打成独立 ERROR 行）。
// 找不到 ERROR 行返回 nil 透传——未知错误形态保守放行。
func (f *InstallFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	content := input.Stdout + input.Stderr
	lines := strings.Split(content, "\n")
	var errs []string
	for _, line := range lines {
		if errorRe.MatchString(strings.TrimRight(line, "\r")) {
			errs = append(errs, line)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return &filter.FilterOutput{Content: strings.Join(errs, "\n") + "\n", Original: content}
}
