package jslint

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

// --- ESLintFilter ---

func TestESLintFilter_Match(t *testing.T) {
	f := &ESLintFilter{}
	if !f.Match("eslint", []string{"."}) {
		t.Error("应匹配 eslint")
	}
	if !f.Match("biome", []string{"check"}) {
		t.Error("应匹配 biome")
	}
	if f.Match("stylelint", []string{"."}) {
		t.Error("不应匹配 stylelint")
	}
}

func TestESLintFilter_Apply_DropsFixableHint(t *testing.T) {
	f := &ESLintFilter{}
	fixture := loadFixture(t, "eslint_stylish.txt")
	out := f.Apply(filter.FilterInput{
		Cmd:      "eslint",
		Args:     []string{"."},
		Stdout:   fixture,
		ExitCode: 1,
	})

	// 关键信息保留
	for _, want := range []string{
		"/Users/app/src/index.ts",
		"semi",
		"@typescript-eslint/no-unused-vars",
		"✖ 9 problems",
	} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留 %q, got:\n%s", want, out.Content)
		}
	}
	// fixable hint 应丢
	if strings.Contains(out.Content, "potentially fixable") {
		t.Errorf("应丢弃 fixable 提示, got:\n%s", out.Content)
	}
}

func TestESLintFilter_Apply_HeadTailTruncates(t *testing.T) {
	f := &ESLintFilter{}
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, "  1:1  error  msg  rule")
	}
	out := f.Apply(filter.FilterInput{
		Cmd:    "eslint",
		Args:   []string{"."},
		Stdout: strings.Join(lines, "\n"),
	})
	if !strings.Contains(out.Content, "行省略") {
		t.Errorf("超长应截断, got lines=%d", strings.Count(out.Content, "\n"))
	}
}

func TestESLintFilter_ApplyOnError_CompressSame(t *testing.T) {
	f := &ESLintFilter{}
	out := f.ApplyOnError(filter.FilterInput{
		Cmd:      "eslint",
		Args:     []string{"."},
		Stdout:   "  1:1  error  Missing semicolon  semi\n✖ 1 problem (1 error, 0 warnings)\n",
		ExitCode: 1,
	})
	if out == nil {
		t.Fatal("eslint 失败应返回非 nil（exit=1 是正常情况，同样压缩）")
	}
}

// --- TscFilter ---

func TestTscFilter_Match(t *testing.T) {
	f := &TscFilter{}
	if !f.Match("tsc", []string{"--noEmit"}) {
		t.Error("应匹配 tsc")
	}
	if f.Match("npx", []string{"tsc"}) {
		t.Error("不应匹配 npx 包装（透明走 npx filter）")
	}
}

func TestTscFilter_Apply_ShortPassThrough(t *testing.T) {
	f := &TscFilter{}
	fixture := loadFixture(t, "tsc_errors.txt")
	out := f.Apply(filter.FilterInput{
		Cmd:    "tsc",
		Args:   []string{"--noEmit"},
		Stdout: fixture,
	})
	// 短 fixture 应透传
	for _, want := range []string{"TS2322", "TS2304", "Found 8 errors"} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留 %q", want)
		}
	}
}

func TestTscFilter_Apply_HeadTail(t *testing.T) {
	f := &TscFilter{}
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, "src/file.ts(1,1): error TS2322: type error")
	}
	out := f.Apply(filter.FilterInput{
		Cmd:    "tsc",
		Args:   []string{"--noEmit"},
		Stdout: strings.Join(lines, "\n"),
	})
	if !strings.Contains(out.Content, "行省略") {
		t.Errorf("超长应截断, got %d 行", strings.Count(out.Content, "\n"))
	}
}

// --- PrettierFilter ---

func TestPrettierFilter_Match(t *testing.T) {
	f := &PrettierFilter{}
	if !f.Match("prettier", []string{"--check", "."}) {
		t.Error("应匹配 prettier")
	}
	if f.Match("prettierd", []string{"."}) {
		t.Error("不应匹配 prettierd（daemon 变体）")
	}
}

func TestPrettierFilter_Apply(t *testing.T) {
	f := &PrettierFilter{}
	fixture := loadFixture(t, "prettier_check.txt")
	out := f.Apply(filter.FilterInput{
		Cmd:      "prettier",
		Args:     []string{"--check", "."},
		Stdout:   fixture,
		ExitCode: 1,
	})
	// 关键行保留
	for _, want := range []string{"[warn] src/components/Button.tsx", "Code style issues found"} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留 %q", want)
		}
	}
	// 开始标记丢
	if strings.Contains(out.Content, "Checking formatting") {
		t.Error("应丢弃 Checking formatting 头行")
	}
}

func TestPrettierFilter_ApplyOnError(t *testing.T) {
	f := &PrettierFilter{}
	out := f.ApplyOnError(filter.FilterInput{
		Cmd:      "prettier",
		Args:     []string{"--check", "."},
		Stdout:   "Checking formatting...\n[warn] a.ts\n",
		ExitCode: 1,
	})
	if out == nil {
		t.Fatal("prettier exit=1 = 发现未格式化文件，正常情况，应返回非 nil")
	}
	if strings.Contains(out.Content, "Checking formatting") {
		t.Error("失败路径也应丢弃 Checking 头")
	}
}
