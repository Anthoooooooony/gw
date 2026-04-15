package hook

import (
	"testing"
)

func TestShouldRewrite(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{"简单命令", "git status", true},
		{"带参数命令", "mvn clean install", true},
		{"管道", "git log | grep fix", false},
		{"双管道(链式)", "mvn test || echo failed", true},
		{"重定向 >", "echo hello > file.txt", false},
		{"追加重定向 >>", "echo hello >> file.txt", false},
		{"输入重定向 <", "cat < file.txt", false},
		{"子 shell", "echo $(date)", false},
		{"反引号", "echo `date`", false},
		{"&& 链式", "mvn clean && mvn test", true},
		{"; 链式", "cd dir; ls", true},
		{"混合链式", "git add . && git commit -m 'msg' || echo fail", true},
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
