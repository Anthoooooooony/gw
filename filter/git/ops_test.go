package git

import (
	"strings"
	"testing"

	"github.com/Anthoooooooony/gw/filter"
)

func TestOpsFilter_Match(t *testing.T) {
	f := &OpsFilter{}

	tests := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"git", []string{"add", "."}, true},
		{"git", []string{"commit", "-m", "msg"}, true},
		{"git", []string{"push"}, true},
		{"git", []string{"pull", "--rebase"}, true},
		{"git", []string{"branch", "-v"}, true},
		{"git", []string{"checkout", "main"}, true},
		// 非 ops 子命令（其他 filter 已覆盖或不需要压缩）
		{"git", []string{"status"}, false},
		{"git", []string{"log"}, false},
		{"git", []string{"diff"}, false},
		{"git", []string{"stash"}, false},
		{"git", nil, false},
		{"hg", []string{"commit"}, false},
	}

	for _, tt := range tests {
		got := f.Match(tt.cmd, tt.args)
		if got != tt.want {
			t.Errorf("Match(%q, %v) = %v, want %v", tt.cmd, tt.args, got, tt.want)
		}
	}
}

func TestOpsFilter_Subname(t *testing.T) {
	f := &OpsFilter{}
	if got := f.Subname("git", []string{"commit", "-m", "x"}); got != "commit" {
		t.Errorf("Subname commit = %q, want %q", got, "commit")
	}
	if got := f.Subname("git", []string{}); got != "" {
		t.Errorf("Subname empty args = %q, want \"\"", got)
	}
	if got := f.Subname("hg", []string{"commit"}); got != "" {
		t.Errorf("Subname non-git = %q, want \"\"", got)
	}
}

func TestOpsFilter_Apply_CommitRoot(t *testing.T) {
	f := &OpsFilter{}
	fixture := loadFixture(t, "git_commit_root.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"commit", "-m", "feat: initial commit"},
		Stdout: fixture,
	})
	// 关键 summary 行应保留
	if !strings.Contains(out.Content, "root-commit") {
		t.Errorf("应保留 root-commit 标记, got:\n%s", out.Content)
	}
	if !strings.Contains(out.Content, "feat: initial commit") {
		t.Errorf("应保留 subject, got:\n%s", out.Content)
	}
	// create mode 行应丢弃
	if strings.Contains(out.Content, "create mode 100644") {
		t.Errorf("应丢弃 create mode 行, got:\n%s", out.Content)
	}
}

func TestOpsFilter_Apply_CommitSimple(t *testing.T) {
	f := &OpsFilter{}
	fixture := loadFixture(t, "git_commit_simple.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"commit", "--allow-empty", "-m", "second commit"},
		Stdout: fixture,
	})
	if !strings.Contains(out.Content, "second commit") {
		t.Errorf("应保留 subject, got:\n%s", out.Content)
	}
}

func TestOpsFilter_Apply_PushFirst(t *testing.T) {
	f := &OpsFilter{}
	fixture := loadFixture(t, "git_push_first.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"push", "-u", "origin", "main"},
		Stdout: fixture,
	})
	for _, want := range []string{"[new branch]", "main -> main", "set up to track"} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留 %q, got:\n%s", want, out.Content)
		}
	}
}

func TestOpsFilter_Apply_PushUpdateStripsNoise(t *testing.T) {
	f := &OpsFilter{}
	// 构造包含典型噪声的 push 输出
	noisy := "Enumerating objects: 5, done.\n" +
		"Counting objects: 100% (5/5), done.\n" +
		"Delta compression using up to 10 threads\n" +
		"Compressing objects: 100% (3/3), done.\n" +
		"Writing objects: 100% (3/3), 300 bytes | 300.00 KiB/s, done.\n" +
		"Total 3 (delta 1), reused 0 (delta 0), pack-reused 0\n" +
		"To ../remote.git\n" +
		"   478d23a..b62cdf0  main -> main\n"

	out := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"push"},
		Stdout: noisy,
	})
	// 关键 summary
	if !strings.Contains(out.Content, "478d23a..b62cdf0  main -> main") {
		t.Errorf("应保留 hash 范围行, got:\n%s", out.Content)
	}
	if !strings.Contains(out.Content, "To ../remote.git") {
		t.Errorf("应保留 remote URL 行, got:\n%s", out.Content)
	}
	// 噪声
	for _, noise := range []string{
		"Enumerating objects",
		"Counting objects",
		"Delta compression",
		"Compressing objects",
		"Writing objects",
		"Total ",
	} {
		if strings.Contains(out.Content, noise) {
			t.Errorf("应丢弃噪声 %q, got:\n%s", noise, out.Content)
		}
	}
}

func TestOpsFilter_Apply_PullUpToDate(t *testing.T) {
	f := &OpsFilter{}
	fixture := loadFixture(t, "git_pull_uptodate.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"pull"},
		Stdout: fixture,
	})
	if !strings.Contains(out.Content, "Already up to date") {
		t.Errorf("应保留 Already up to date, got:\n%s", out.Content)
	}
}

func TestOpsFilter_Apply_PullMerge(t *testing.T) {
	f := &OpsFilter{}
	fixture := loadFixture(t, "git_pull_merge.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"pull"},
		Stdout: fixture,
	})
	for _, want := range []string{
		"main       -> origin/main",
		"Merge made",
		"a.txt | 1 +",
		"1 file changed",
	} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留 %q, got:\n%s", want, out.Content)
		}
	}
}

func TestOpsFilter_Apply_Branch(t *testing.T) {
	f := &OpsFilter{}
	fixture := loadFixture(t, "git_branch_v.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"branch", "-v"},
		Stdout: fixture,
	})
	if !strings.Contains(out.Content, "* main") {
		t.Errorf("应保留当前分支行, got:\n%s", out.Content)
	}
	if !strings.Contains(out.Content, "feature/foo") || !strings.Contains(out.Content, "feature/bar") {
		t.Errorf("应保留所有分支, got:\n%s", out.Content)
	}
}

func TestOpsFilter_Apply_CheckoutNew(t *testing.T) {
	f := &OpsFilter{}
	fixture := loadFixture(t, "git_checkout_newbranch.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"checkout", "-b", "newthing"},
		Stdout: fixture,
	})
	if !strings.Contains(out.Content, "Switched to a new branch 'newthing'") {
		t.Errorf("应保留 switched 行, got:\n%s", out.Content)
	}
}

func TestOpsFilter_Apply_CheckoutExisting(t *testing.T) {
	f := &OpsFilter{}
	fixture := loadFixture(t, "git_checkout_existing.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"checkout", "main"},
		Stdout: fixture,
	})
	if !strings.Contains(out.Content, "Switched to branch 'main'") {
		t.Errorf("应保留 switched 行, got:\n%s", out.Content)
	}
}

func TestOpsFilter_Apply_AddEmpty(t *testing.T) {
	f := &OpsFilter{}
	// git add 成功通常无 stdout
	out := f.Apply(filter.FilterInput{
		Cmd:    "git",
		Args:   []string{"add", "."},
		Stdout: "",
	})
	if out.Content != "" {
		t.Errorf("空输入应透传空, got %q", out.Content)
	}
}

func TestOpsFilter_ApplyOnError(t *testing.T) {
	f := &OpsFilter{}
	// 失败透传（返回 nil）—— merge conflict / rejected / auth failed 信息短且关键，盲压缩风险大
	if out := f.ApplyOnError(filter.FilterInput{ExitCode: 1}); out != nil {
		t.Errorf("ApplyOnError 应返回 nil 透传, got %+v", out)
	}
}
