package git

import (
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&OpsFilter{})
}

// gitOpsSubcmds 本 filter 接管的 git 子命令。
// 共性：输出多为"进度计数 + 关键 summary"的混合，压缩策略是丢进度保 summary。
var gitOpsSubcmds = map[string]bool{
	"add":      true,
	"commit":   true,
	"push":     true,
	"pull":     true,
	"branch":   true,
	"checkout": true,
}

// OpsFilter 压缩 git add/commit/push/pull/branch/checkout 的进度噪声，保留 summary。
type OpsFilter struct{}

func (f *OpsFilter) Name() string { return "git/ops" }

func (f *OpsFilter) Match(cmd string, args []string) bool {
	if cmd != "git" || len(args) == 0 {
		return false
	}
	return gitOpsSubcmds[args[0]]
}

// Subname 让 FilterUsed 展示为 "git/ops/commit" 等，诊断友好。
func (f *OpsFilter) Subname(cmd string, args []string) string {
	if cmd != "git" || len(args) == 0 {
		return ""
	}
	if gitOpsSubcmds[args[0]] {
		return args[0]
	}
	return ""
}

// gitNoisePrefixes 是所有 git op 共有的进度/计数行前缀——这些对 LLM 是冗余。
// 保守策略：只丢已知噪声，未知行一律保留（filter invariant）。
var gitNoisePrefixes = []string{
	"Enumerating objects:",
	"Counting objects:",
	"Delta compression using",
	"Compressing objects:",
	"Writing objects:",
	"Receiving objects:",
	"Resolving deltas:",
	"Total ",
	"remote: Enumerating",
	"remote: Counting",
	"remote: Compressing",
	"remote: Total",
	"remote: Resolving",
}

// commitOnlyDropPrefixes 是 commit 特有的可丢行（文件模式元信息，LLM 基本用不到）。
var commitOnlyDropPrefixes = []string{
	" create mode ",
	" delete mode ",
	" rename ",
	" mode change ",
}

func (f *OpsFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	content := filter.StripANSI(original)
	subcmd := f.Subname(input.Cmd, input.Args)

	lines := strings.Split(content, "\n")
	var out []string
	for _, line := range lines {
		if isGitNoise(line) {
			continue
		}
		if subcmd == "commit" && isCommitNoise(line) {
			continue
		}
		out = append(out, line)
	}

	joined := strings.Join(out, "\n")
	return filter.FilterOutput{
		Content:  joined,
		Original: original,
	}
}

func (f *OpsFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	// 失败透传：git op 失败信息通常本来就短且关键（merge conflict / rejected / auth failed），
	// 盲压缩风险大于收益。
	return nil
}

func isGitNoise(line string) bool {
	for _, p := range gitNoisePrefixes {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

func isCommitNoise(line string) bool {
	for _, p := range commitOnlyDropPrefixes {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}
