package toml

import (
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

func makeFilter(rules ...Rule) *TomlFilter {
	return &TomlFilter{Rules: rules}
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

func TestStripLines(t *testing.T) {
	f := makeFilter(Rule{Match: "test", StripLines: []string{"^DEBUG"}})
	input := filter.FilterInput{
		Cmd:    "test",
		Args:   []string{},
		Stdout: "DEBUG: something\nINFO: hello\nDEBUG: another\nERROR: bad",
	}
	out := f.Apply(input)
	lines := strings.Split(out.Content, "\n")
	if len(lines) != 2 {
		t.Errorf("期望 2 行，得到 %d 行: %v", len(lines), lines)
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "DEBUG") {
			t.Errorf("DEBUG 行未被移除: %s", line)
		}
	}
}

func TestKeepLines(t *testing.T) {
	f := makeFilter(Rule{Match: "test", KeepLines: []string{"ERROR"}})
	input := filter.FilterInput{
		Cmd:    "test",
		Args:   []string{},
		Stdout: "INFO: ok\nERROR: bad\nDEBUG: trace\nERROR: worse",
	}
	out := f.Apply(input)
	lines := strings.Split(out.Content, "\n")
	if len(lines) != 2 {
		t.Errorf("期望 2 行，得到 %d 行: %v", len(lines), lines)
	}
	for _, line := range lines {
		if !strings.Contains(line, "ERROR") {
			t.Errorf("非 ERROR 行未被过滤: %s", line)
		}
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
	f := makeFilter(Rule{Match: "test", KeepLines: []string{"NOTFOUND"}, OnEmpty: "无匹配输出"})
	input := filter.FilterInput{
		Cmd:    "test",
		Args:   []string{},
		Stdout: "INFO: ok\nDEBUG: trace",
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

// TestApplyOnError_NoRule 未命中任何规则时 ApplyOnError 返回 nil
// （上层继续透传原始输出）。
func TestApplyOnError_NoRule(t *testing.T) {
	f := makeFilter(Rule{Match: "other"})
	input := filter.FilterInput{
		Cmd:      "test",
		Args:     []string{},
		Stdout:   "output",
		ExitCode: 1,
	}
	if result := f.ApplyOnError(input); result != nil {
		t.Errorf("未命中规则时应返回 nil, 得到 %+v", result)
	}
}

// TestApplyOnError_NoOnErrorConfig 命中规则但未配置 on_error 时仍返回 nil，
// 保留历史 pass-through 语义。
func TestApplyOnError_NoOnErrorConfig(t *testing.T) {
	f := makeFilter(Rule{Match: "test"})
	input := filter.FilterInput{
		Cmd:      "test",
		Args:     []string{},
		Stdout:   "output",
		ExitCode: 1,
	}
	if result := f.ApplyOnError(input); result != nil {
		t.Errorf("rule.OnError 为 nil 时应返回 nil, 得到 %+v", result)
	}
}

// TestApplyOnError_WithOnError 配置了 on_error 子规则时按其 strip/tail 管道处理
// 合并后的 Stdout+Stderr。
func TestApplyOnError_WithOnError(t *testing.T) {
	f := makeFilter(Rule{
		Match: "test",
		OnError: &Rule{
			StripLines: []string{"^DEBUG"},
			TailLines:  3,
		},
	})
	input := filter.FilterInput{
		Cmd:      "test",
		Args:     []string{},
		Stdout:   "DEBUG: noise\nline1\nline2\n",
		Stderr:   "line3\nline4",
		ExitCode: 1,
	}
	out := f.ApplyOnError(input)
	if out == nil {
		t.Fatal("配置了 OnError 应返回非 nil")
	}
	// 合并后 = "DEBUG: noise\nline1\nline2\nline3\nline4"
	// strip DEBUG → [line1, line2, line3, line4]
	// tail 3     → [line2, line3, line4]
	wantLines := []string{"line2", "line3", "line4"}
	got := strings.Split(out.Content, "\n")
	if len(got) != len(wantLines) {
		t.Fatalf("期望 %d 行, 得到 %d 行: %q", len(wantLines), len(got), out.Content)
	}
	for i, w := range wantLines {
		if got[i] != w {
			t.Errorf("line[%d] = %q, want %q", i, got[i], w)
		}
	}
	// Original 应为合并后的原始输入
	wantOrig := input.Stdout + input.Stderr
	if out.Original != wantOrig {
		t.Errorf("Original = %q, want %q", out.Original, wantOrig)
	}
}

// TestLoadOnErrorFromTOML 确认 loader 能把 [section.name.on_error] 子表
// 递归解析为 Rule.OnError。
func TestLoadOnErrorFromTOML(t *testing.T) {
	data := `
[demo.run]
match = "demo"
strip_lines = ["^ok"]

  [demo.run.on_error]
  strip_lines = ["^progress"]
  tail_lines = 42
`
	byID := map[string]LoadedRule{}
	disabled := map[string]bool{}
	parseAndMerge(data, "test", byID, disabled)
	lr, ok := byID["demo.run"]
	if !ok {
		t.Fatal("应解析到 demo.run 规则")
	}
	if lr.Rule.OnError == nil {
		t.Fatal("Rule.OnError 不应为 nil")
	}
	if lr.Rule.OnError.TailLines != 42 {
		t.Errorf("OnError.TailLines = %d, want 42", lr.Rule.OnError.TailLines)
	}
	if len(lr.Rule.OnError.StripLines) != 1 || lr.Rule.OnError.StripLines[0] != "^progress" {
		t.Errorf("OnError.StripLines = %+v, want [^progress]", lr.Rule.OnError.StripLines)
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

func TestLoadBuiltinRules(t *testing.T) {
	f, err := LoadBuiltinRules()
	if err != nil {
		t.Fatalf("加载内置规则失败: %v", err)
	}
	if len(f.Rules) == 0 {
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
