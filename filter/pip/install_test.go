package pip

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Anthoooooooony/gw/filter"
)

func TestInstallMatch(t *testing.T) {
	cases := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"pip", []string{"install", "attrs"}, true},
		{"pip3", []string{"install", "requests"}, true},
		{"python", []string{"-m", "pip", "install", "numpy"}, true},
		{"python3", []string{"-m", "pip", "install", "-e", "."}, true},
		{"python", []string{"-mpip", "install", "pkg"}, true},
		{"pip", []string{"list"}, false},
		{"pip", []string{"show", "attrs"}, false},
		{"python", []string{"-m", "venv", ".venv"}, false},
		{"python", []string{"-m", "pip", "list"}, false},
		{"uv", []string{"pip", "install", "attrs"}, false},
		{"pip", []string{}, false},
	}
	f := &InstallFilter{}
	for _, c := range cases {
		if got := f.Match(c.cmd, c.args); got != c.want {
			t.Errorf("Match(%q, %v) = %v, want %v", c.cmd, c.args, got, c.want)
		}
	}
}

func TestInstallApply_Success(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "toml", "testdata", "pip_install.txt"))
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	out := (&InstallFilter{}).Apply(filter.FilterInput{
		Cmd:    "pip",
		Args:   []string{"install", "-e", "."},
		Stdout: string(data),
	})
	if !strings.HasPrefix(out.Content, "Successfully installed ") {
		t.Fatalf("应保留 Successfully installed 行, got %q", out.Content)
	}
	for _, banned := range []string{"Obtaining", "Installing build dependencies", "Building wheels"} {
		if strings.Contains(out.Content, banned) {
			t.Errorf("安装进度行 %q 应被丢弃", banned)
		}
	}
}

func TestInstallApply_NoAnchor_Fallback(t *testing.T) {
	out := (&InstallFilter{}).Apply(filter.FilterInput{
		Cmd:    "pip",
		Args:   []string{"install", "x"},
		Stdout: "Obtaining x\nNot matching package\n",
	})
	if !strings.Contains(out.Content, "Obtaining x") {
		t.Errorf("无 Successfully 行应原文透传, got %q", out.Content)
	}
}

func TestInstallApplyOnError_KeepsErrorLines(t *testing.T) {
	input := `Collecting nonexistent-pkg-xyz
ERROR: Could not find a version that satisfies the requirement nonexistent-pkg-xyz
ERROR: No matching distribution found for nonexistent-pkg-xyz
`
	out := (&InstallFilter{}).ApplyOnError(filter.FilterInput{
		Cmd:      "pip",
		Args:     []string{"install", "nonexistent-pkg-xyz"},
		Stdout:   input,
		ExitCode: 1,
	})
	if out == nil {
		t.Fatal("应返回非 nil")
	}
	if strings.Contains(out.Content, "Collecting nonexistent-pkg-xyz") {
		t.Error("非 ERROR 行应被丢弃")
	}
	if !strings.Contains(out.Content, "Could not find a version") {
		t.Error("ERROR 行应保留")
	}
	if !strings.Contains(out.Content, "No matching distribution") {
		t.Error("ERROR 行应全部保留")
	}
}

func TestInstallApplyOnError_NoError_Nil(t *testing.T) {
	out := (&InstallFilter{}).ApplyOnError(filter.FilterInput{
		Cmd:      "pip",
		Args:     []string{"install", "x"},
		Stdout:   "Collecting x\nSome non-ERROR line\n",
		ExitCode: 1,
	})
	if out != nil {
		t.Errorf("无 ERROR 行应返回 nil, got %+v", out)
	}
}
