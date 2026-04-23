package gh

import (
	"os"
	"strings"
	"testing"

	"github.com/Anthoooooooony/gw/filter"
)

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("无法加载 %s: %v", name, err)
	}
	return string(data)
}

func TestFilter_Match(t *testing.T) {
	f := &Filter{}
	tests := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"gh", []string{"pr", "list"}, true},
		{"gh", []string{"pr", "view", "127"}, true},
		{"gh", []string{"issue", "list"}, true},
		{"gh", []string{"issue", "view", "42"}, true},
		{"gh", []string{"run", "list"}, true},
		{"gh", []string{"workflow", "list"}, true},
		// 不接管的场景
		{"gh", []string{"pr", "create"}, false},
		{"gh", []string{"pr", "merge"}, false},
		{"gh", []string{"pr", "checkout"}, false},
		{"gh", []string{"auth", "login"}, false},
		{"gh", []string{}, false},
		{"gh", []string{"pr"}, false},
		// --json 输出结构化数据，盲压缩风险大，透传
		{"gh", []string{"pr", "list", "--json", "number,title"}, false},
		{"gh", []string{"pr", "view", "127", "--json=body"}, false},
		// 非 gh
		{"git", []string{"pr", "list"}, false},
	}
	for _, tt := range tests {
		if got := f.Match(tt.cmd, tt.args); got != tt.want {
			t.Errorf("Match(%q, %v) = %v, want %v", tt.cmd, tt.args, got, tt.want)
		}
	}
}

func TestFilter_Subname(t *testing.T) {
	f := &Filter{}
	if got := f.Subname("gh", []string{"pr", "view", "127"}); got != "pr/view" {
		t.Errorf("Subname pr view = %q, want pr/view", got)
	}
	if got := f.Subname("gh", []string{"run", "list"}); got != "run/list" {
		t.Errorf("Subname run list = %q, want run/list", got)
	}
	if got := f.Subname("gh", nil); got != "" {
		t.Errorf("empty args = %q, want \"\"", got)
	}
}

func TestFilter_Apply_PrView_StripsEmptyFields(t *testing.T) {
	f := &Filter{}
	fixture := loadFixture(t, "gh_pr_view.txt")
	out := f.Apply(filter.FilterInput{
		Cmd:    "gh",
		Args:   []string{"pr", "view", "127"},
		Stdout: fixture,
	})

	// 关键字段保留
	for _, want := range []string{"title:", "state:", "author:", "number:", "url:"} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留关键字段 %q", want)
		}
	}
	// 空字段丢弃
	for _, noise := range []string{"labels:\t\n", "assignees:\t\n", "reviewers:\t\n", "projects:\t\n", "milestone:\t\n"} {
		if strings.Contains(out.Content, noise) {
			t.Errorf("应丢弃空字段 %q", noise)
		}
	}
	// body（在 -- 之后）保留
	if !strings.Contains(out.Content, "--") {
		t.Error("应保留 body 分隔符 --")
	}
	if !strings.Contains(out.Content, "Summary") {
		t.Error("应保留 body 内容")
	}
	// 应有压缩
	if len(out.Content) >= len(fixture) {
		t.Errorf("应产生压缩: got %d >= fixture %d", len(out.Content), len(fixture))
	}
}

func TestFilter_Apply_PrView_KeepsFilledFields(t *testing.T) {
	f := &Filter{}
	// label 不为空时不应被误丢
	input := "title:\tfoo\nstate:\tOPEN\nlabels:\tbug, help wanted\nassignees:\nreviewers:\tbob\n--\nbody\n"
	out := f.Apply(filter.FilterInput{
		Cmd:    "gh",
		Args:   []string{"pr", "view"},
		Stdout: input,
	})
	if !strings.Contains(out.Content, "labels:\tbug, help wanted") {
		t.Errorf("非空 labels 应保留, got:\n%s", out.Content)
	}
	if !strings.Contains(out.Content, "reviewers:\tbob") {
		t.Errorf("非空 reviewers 应保留, got:\n%s", out.Content)
	}
	if strings.Contains(out.Content, "assignees:\n") {
		t.Errorf("空 assignees 应丢弃, got:\n%s", out.Content)
	}
}

func TestFilter_Apply_PrList_ShortPassThrough(t *testing.T) {
	f := &Filter{}
	fixture := loadFixture(t, "gh_pr_list.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "gh",
		Args:   []string{"pr", "list", "--state", "all"},
		Stdout: fixture,
	})
	// 20 行小于阈值，应透传
	if out.Content != fixture {
		t.Errorf("短 list 应透传\ngot  %q\nwant %q", out.Content, fixture)
	}
}

func TestFilter_Apply_PrList_LongTruncates(t *testing.T) {
	f := &Filter{}
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "123\ttitle\tbranch\tMERGED\t2026-04-22T00:00:00Z")
	}
	input := strings.Join(lines, "\n")
	out := f.Apply(filter.FilterInput{
		Cmd:    "gh",
		Args:   []string{"pr", "list"},
		Stdout: input,
	})
	if !strings.Contains(out.Content, "行省略") {
		t.Errorf("超长 list 应截断, got %d 行", strings.Count(out.Content, "\n"))
	}
	if strings.Count(out.Content, "\n") > 100 {
		t.Errorf("应压缩到 ~80 行, got %d", strings.Count(out.Content, "\n"))
	}
}

func TestFilter_Apply_IssueList(t *testing.T) {
	f := &Filter{}
	fixture := loadFixture(t, "gh_issue_list.txt")
	out := f.Apply(filter.FilterInput{
		Cmd:    "gh",
		Args:   []string{"issue", "list"},
		Stdout: fixture,
	})
	// 10 行 < 阈值，透传
	if out.Content != fixture {
		t.Errorf("短 issue list 应透传")
	}
}

func TestFilter_Apply_RunList(t *testing.T) {
	f := &Filter{}
	fixture := loadFixture(t, "gh_run_list.txt")
	out := f.Apply(filter.FilterInput{
		Cmd:    "gh",
		Args:   []string{"run", "list"},
		Stdout: fixture,
	})
	// 10 行 < 阈值，透传
	if out.Content != fixture {
		t.Errorf("短 run list 应透传")
	}
}

func TestFilter_ApplyOnError(t *testing.T) {
	f := &Filter{}
	if out := f.ApplyOnError(filter.FilterInput{ExitCode: 1}); out != nil {
		t.Errorf("ApplyOnError 应返回 nil, got %+v", out)
	}
}
