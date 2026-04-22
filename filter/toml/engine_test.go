package toml

import (
	"strings"
	"testing"

	"github.com/Anthoooooooony/gw/filter"
)

func makeFilter(rules ...Rule) *TomlFilter {
	loaded := make([]LoadedRule, len(rules))
	for i, r := range rules {
		loaded[i] = LoadedRule{ID: r.Match, Rule: r, Source: SourceBuiltin}
	}
	return &TomlFilter{Loaded: loaded}
}

func TestMatch(t *testing.T) {
	f := makeFilter(Rule{Match: "docker ps"})

	if !f.Match("docker", []string{"ps"}) {
		t.Error("应匹配 docker ps")
	}
	if !f.Match("docker", []string{"ps", "-a"}) {
		t.Error("应匹配 docker ps -a（前缀匹配）")
	}
	if f.Match("docker", []string{"build", "."}) {
		t.Error("不应匹配 docker build")
	}
}

func TestMaxLines(t *testing.T) {
	f := makeFilter(Rule{Match: "test", MaxLines: 3})
	input := filter.FilterInput{
		Cmd:    "test",
		Args:   []string{},
		Stdout: "line1\nline2\nline3\nline4\nline5",
	}
	out := f.Apply(input)
	lines := strings.Split(out.Content, "\n")
	if len(lines) != 3 {
		t.Errorf("期望 3 行，得到 %d 行", len(lines))
	}
	if lines[0] != "line1" || lines[2] != "line3" {
		t.Errorf("截断内容不正确: %v", lines)
	}
}

func TestTailLines(t *testing.T) {
	f := makeFilter(Rule{Match: "test", TailLines: 2})
	input := filter.FilterInput{
		Cmd:    "test",
		Args:   []string{},
		Stdout: "line1\nline2\nline3\nline4\nline5",
	}
	out := f.Apply(input)
	lines := strings.Split(out.Content, "\n")
	if len(lines) != 2 {
		t.Errorf("期望 2 行，得到 %d 行", len(lines))
	}
	if lines[0] != "line4" || lines[1] != "line5" {
		t.Errorf("tail 内容不正确: %v", lines)
	}
}

func TestStripAnsi(t *testing.T) {
	f := makeFilter(Rule{Match: "test", StripAnsi: true})
	input := filter.FilterInput{
		Cmd:    "test",
		Args:   []string{},
		Stdout: "\x1b[31mred text\x1b[0m normal",
	}
	out := f.Apply(input)
	if strings.Contains(out.Content, "\x1b[") {
		t.Errorf("ANSI 转义码未被移除: %q", out.Content)
	}
	if out.Content != "red text normal" {
		t.Errorf("内容不正确: %q", out.Content)
	}
}

func TestOnEmpty(t *testing.T) {
	// 空输入 + on_empty 配置 → 返回 on_empty 文案
	f := makeFilter(Rule{Match: "test", OnEmpty: "无匹配输出"})
	input := filter.FilterInput{
		Cmd:    "test",
		Args:   []string{},
		Stdout: "   \n  \n",
	}
	out := f.Apply(input)
	if out.Content != "无匹配输出" {
		t.Errorf("on_empty 未生效: %q", out.Content)
	}
}

func TestLongestMatch(t *testing.T) {
	f := makeFilter(
		Rule{Match: "docker", MaxLines: 100},
		Rule{Match: "docker ps", MaxLines: 3},
	)
	input := filter.FilterInput{
		Cmd:    "docker",
		Args:   []string{"ps", "-a"},
		Stdout: "line1\nline2\nline3\nline4\nline5",
	}
	out := f.Apply(input)
	lines := strings.Split(out.Content, "\n")
	if len(lines) != 3 {
		t.Errorf("应使用最长匹配规则(max_lines=3)，得到 %d 行", len(lines))
	}
}

func TestApplyOnError(t *testing.T) {
	f := makeFilter(Rule{Match: "test"})
	input := filter.FilterInput{
		Cmd:      "test",
		Args:     []string{},
		Stdout:   "output",
		ExitCode: 1,
	}
	if result := f.ApplyOnError(input); result != nil {
		t.Error("ApplyOnError 应返回 nil")
	}
}

// TestSubname_PureFunction 验证 Subname 是纯函数，不依赖/不修改实例状态。
// 交错调用不同命令时 Subname 各自返回正确子名，不被"上次调用"污染。
func TestSubname_PureFunction(t *testing.T) {
	f := makeFilter(
		Rule{Match: "docker ps"},
		Rule{Match: "docker images"},
	)

	// TomlFilter 必须实现 SubnameResolver（编译期 assert）
	var _ filter.SubnameResolver = f

	if got := f.Subname("docker", []string{"ps"}); got != "docker ps" {
		t.Errorf("Subname(docker ps) = %q, 期望 docker ps", got)
	}
	if got := f.Subname("docker", []string{"images"}); got != "docker images" {
		t.Errorf("Subname(docker images) = %q, 期望 docker images", got)
	}
	// 再次调 docker ps 仍应返回 docker ps（不被 images 污染）
	if got := f.Subname("docker", []string{"ps"}); got != "docker ps" {
		t.Errorf("重复调用后 Subname(docker ps) = %q, 期望 docker ps", got)
	}
	if got := f.Subname("docker", []string{"nope"}); got != "" {
		t.Errorf("Subname(未匹配) = %q, 期望空", got)
	}
}

// TestSubname_NoRaceUnderConcurrency 多 goroutine 并发 Match + Subname，
// go test -race 必须通过。回归测试 #56 的 matchedRule race。
func TestSubname_NoRaceUnderConcurrency(t *testing.T) {
	f := makeFilter(
		Rule{Match: "docker ps"},
		Rule{Match: "docker images"},
	)

	const workers = 16
	const itersPerWorker = 200
	done := make(chan struct{}, workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer func() { done <- struct{}{} }()
			subcmds := []string{"ps", "images"}
			for i := 0; i < itersPerWorker; i++ {
				sub := subcmds[i%2]
				if !f.Match("docker", []string{sub}) {
					t.Errorf("w=%d i=%d: Match 应成功", w, i)
					return
				}
				want := "docker " + sub
				if got := f.Subname("docker", []string{sub}); got != want {
					t.Errorf("w=%d i=%d: Subname = %q, want %q", w, i, got, want)
					return
				}
			}
		}(w)
	}
	for i := 0; i < workers; i++ {
		<-done
	}
}

func TestLoadEngineBuiltinRules(t *testing.T) {
	f := LoadEngine()
	if len(f.Loaded) == 0 {
		t.Error("内置规则为空")
	}
	// 验证 docker ps 规则存在
	if !f.Match("docker", []string{"ps"}) {
		t.Error("内置规则应匹配 docker ps")
	}
	// 验证 kubectl get 规则存在
	if !f.Match("kubectl", []string{"get", "pods"}) {
		t.Error("内置规则应匹配 kubectl get pods")
	}
}
