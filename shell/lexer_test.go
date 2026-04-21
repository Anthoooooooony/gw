package shell

import (
	"testing"
)

func TestShouldRewrite(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		// 基本命令
		{"简单命令", "git status", true},
		{"带参数命令", "mvn clean install", true},

		// 管道
		{"真正的管道", "git log | grep fix", false},
		{"双引号内的管道", `git log --format="%H|%s"`, true},
		{"单引号内的管道", `echo 'a|b'`, true},

		// 链式操作符
		{"双管道(链式)", "mvn test || echo failed", true},
		{"&& 链式", "mvn clean && mvn test", true},
		{"; 链式", "cd dir; ls", true},
		{"混合链式", "git add . && git commit -m 'msg' || echo fail", true},

		// 重定向
		{"重定向 >", "mvn test > output.txt", false},
		{"追加重定向 >>", "echo hello >> file.txt", false},
		{"输入重定向 <", "cat < file.txt", false},
		{"单引号内的重定向", `echo 'hello > world'`, true},
		{"双引号内的重定向", `echo "hello > world"`, true},

		// 子 shell
		{"子 shell $()", "echo $(date)", false},
		{"双引号内的 $()", `echo "$(date)"`, true},
		{"反引号", "echo `date`", false},

		// 后台执行
		{"后台 &", "sleep 10 &", false},

		// 转义字符
		{"转义的管道", `echo hello \| world`, true},
		{"转义的重定向", `echo hello \> world`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldRewrite(tt.command)
			if got != tt.want {
				t.Errorf("ShouldRewrite(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestSplitChainedCommands(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []Segment
	}{
		{
			"单个命令",
			"git status",
			[]Segment{{Cmd: "git status", Sep: ""}},
		},
		{
			"&& 链式",
			"mvn clean && mvn test",
			[]Segment{
				{Cmd: "mvn clean", Sep: "&&"},
				{Cmd: "mvn test", Sep: ""},
			},
		},
		{
			"|| 和 ; 混合",
			"git add . || echo fail ; ls",
			[]Segment{
				{Cmd: "git add .", Sep: "||"},
				{Cmd: "echo fail", Sep: ";"},
				{Cmd: "ls", Sep: ""},
			},
		},
		{
			"三段 &&",
			"cd dir && mvn clean && mvn test",
			[]Segment{
				{Cmd: "cd dir", Sep: "&&"},
				{Cmd: "mvn clean", Sep: "&&"},
				{Cmd: "mvn test", Sep: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitChainedCommands(tt.command)
			if len(got) != len(tt.want) {
				t.Fatalf("SplitChainedCommands(%q) got %d segments, want %d", tt.command, len(got), len(tt.want))
			}
			for i := range got {
				if got[i].Cmd != tt.want[i].Cmd || got[i].Sep != tt.want[i].Sep {
					t.Errorf("segment[%d] = {%q, %q}, want {%q, %q}",
						i, got[i].Cmd, got[i].Sep, tt.want[i].Cmd, tt.want[i].Sep)
				}
			}
		})
	}
}

func TestAnalyzeCommand_EdgeCases(t *testing.T) {
	tests := []struct {
		cmd        string
		canRewrite bool
		segments   int
	}{
		// Nested quotes: single quote containing double quote
		{`echo '"hello"'`, true, 1},
		// Double quote containing single quote
		{`echo "it's fine"`, true, 1},
		// Empty command
		{"", false, 0},
		// Only whitespace
		{"   ", false, 0},
		// Escaped quote
		{`echo \"hello\"`, true, 1},
		// && inside quotes (should NOT split)
		{`echo "a && b"`, true, 1},
		// Multiple chains
		{"cmd1 && cmd2 || cmd3 ; cmd4", true, 4},
		// Pipe inside single quotes
		{`grep 'foo|bar' file.txt`, true, 1},
		// $( inside single quotes (single quotes don't expand)
		{`echo '$(date)'`, true, 1},
		// Real-world: git log with format
		{`git log --pretty=format:"%H|%an|%s" -10`, true, 1},
		// Real-world: mvn with -D property containing special chars
		{`mvn test -Dtest="MyTest#method"`, true, 1},
	}
	for _, tt := range tests {
		canRewrite, segments := AnalyzeCommand(tt.cmd)
		if canRewrite != tt.canRewrite {
			t.Errorf("AnalyzeCommand(%q) canRewrite = %v, want %v", tt.cmd, canRewrite, tt.canRewrite)
		}
		if canRewrite && len(segments) != tt.segments {
			t.Errorf("AnalyzeCommand(%q) segments = %d, want %d", tt.cmd, len(segments), tt.segments)
		}
	}
}

func TestAnalyzeCommand(t *testing.T) {
	tests := []struct {
		name         string
		command      string
		wantRewrite  bool
		wantSegCount int
	}{
		// 核心修复：引号内的管道不应被拒绝
		{
			"双引号内管道允许改写",
			`git log --format="%H|%s"`,
			true, 1,
		},
		// 单引号内的 && 不是分隔符
		{
			"单引号内 && 不分割",
			`echo 'a&&b' && echo c`,
			true, 2,
		},
		// 真正的管道拒绝改写
		{
			"管道拒绝改写",
			"git log | head",
			false, 0,
		},
		// 复杂引号场景
		{
			"混合引号和链式",
			`git commit -m "fix: a && b" && git push`,
			true, 2,
		},
		// 空命令
		{
			"空命令",
			"",
			false, 0,
		},
		// 纯空白
		{
			"纯空白",
			"   ",
			false, 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canRewrite, segments := AnalyzeCommand(tt.command)
			if canRewrite != tt.wantRewrite {
				t.Errorf("AnalyzeCommand(%q) canRewrite = %v, want %v", tt.command, canRewrite, tt.wantRewrite)
			}
			segCount := len(segments)
			if canRewrite && segCount != tt.wantSegCount {
				t.Errorf("AnalyzeCommand(%q) got %d segments, want %d", tt.command, segCount, tt.wantSegCount)
			}
			if !canRewrite && segments != nil {
				t.Errorf("AnalyzeCommand(%q) canRewrite=false but segments != nil", tt.command)
			}
		})
	}
}

func TestTokenizeSegment(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   ", nil},
		{"simple", "git status", []string{"git", "status"}},
		{"trim extra spaces", "  git   status  ", []string{"git", "status"}},
		{"double-quoted arg with spaces", `git commit -m "fix: foo bar"`, []string{"git", "commit", "-m", "fix: foo bar"}},
		{"single-quoted arg", `git log --grep='fix bar'`, []string{"git", "log", "--grep=fix bar"}},
		// 双引号内 `\<x>` 与 AnalyzeCommand 保持一致：escape 任意字符（gw 只识别子命令，
		// 不严格还原 bash 的"仅 $ ` \" \\ newline 才是 escape"精细语义）
		{"escaped space in double quotes", `git commit -m "line\ break"`, []string{"git", "commit", "-m", "line break"}},
		{"backslash escaped space outside quotes", `echo hello\ world`, []string{"echo", "hello world"}},
		{"nested quotes different types", `echo "foo 'inner' bar"`, []string{"echo", "foo 'inner' bar"}},
		{"tabs as separator", "git\tstatus", []string{"git", "status"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TokenizeSegment(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("TokenizeSegment(%q) len=%d tokens=%v, want len=%d tokens=%v",
					tt.in, len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("TokenizeSegment(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}
