package jsbuild

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

// --- PlaywrightFilter ---

func TestPlaywrightFilter_Match(t *testing.T) {
	f := &PlaywrightFilter{}
	if !f.Match("playwright", []string{"test"}) {
		t.Error("应匹配 playwright")
	}
	if f.Match("pw", []string{"test"}) {
		t.Error("不应匹配 pw")
	}
}

func TestPlaywrightFilter_Apply_Success_DropsPassLines(t *testing.T) {
	f := &PlaywrightFilter{}
	fixture := loadFixture(t, "playwright_success.txt")
	out := f.Apply(filter.FilterInput{
		Cmd:    "playwright",
		Args:   []string{"test"},
		Stdout: fixture,
	})
	// summary 保留
	if !strings.Contains(out.Content, "12 passed") {
		t.Errorf("应保留 summary, got:\n%s", out.Content)
	}
	// ✓ 通过行应丢
	if strings.Contains(out.Content, "[chromium] › tests/login") {
		t.Error("✓ 通过行应被丢弃")
	}
	// Slow test file 丢
	if strings.Contains(out.Content, "Slow test file") {
		t.Error("Slow test file 应丢")
	}
	// 应有显著压缩
	if len(out.Content) >= len(fixture) {
		t.Errorf("应有压缩: got %d >= fixture %d", len(out.Content), len(fixture))
	}
}

func TestPlaywrightFilter_ApplyOnError_KeepsFailureDetails(t *testing.T) {
	f := &PlaywrightFilter{}
	fixture := loadFixture(t, "playwright_failure.txt")
	out := f.ApplyOnError(filter.FilterInput{
		Cmd:      "playwright",
		Args:     []string{"test"},
		Stdout:   fixture,
		ExitCode: 1,
	})
	if out == nil {
		t.Fatal("playwright 失败应返回非 nil")
	}
	// 失败详情保留
	for _, want := range []string{
		"1) [chromium]",
		"Expected: \"$100.00\"",
		"Received: \"$99.99\"",
		"2 failed",
		"6 passed",
	} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留 %q, got:\n%s", want, out.Content)
		}
	}
	// ✓ 通过行丢
	if strings.Contains(out.Content, "✓  1 [chromium]") {
		t.Error("✓ 通过行应丢")
	}
}

// --- PrismaFilter ---

func TestPrismaFilter_Match(t *testing.T) {
	f := &PrismaFilter{}
	if !f.Match("prisma", []string{"generate"}) {
		t.Error("应匹配 prisma")
	}
	if f.Match("prismac", []string{"generate"}) {
		t.Error("不应匹配 prismac")
	}
}

func TestPrismaFilter_Apply_Generate(t *testing.T) {
	f := &PrismaFilter{}
	fixture := loadFixture(t, "prisma_generate.txt")
	out := f.Apply(filter.FilterInput{
		Cmd:    "prisma",
		Args:   []string{"generate"},
		Stdout: fixture,
	})
	// 关键信息保留
	if !strings.Contains(out.Content, "Generated Prisma Client") {
		t.Errorf("应保留 generation summary, got:\n%s", out.Content)
	}
	// 噪声丢
	for _, noise := range []string{
		"Environment variables loaded",
		"Prisma schema loaded",
		"Tip: Want real-time",
		"Start by importing",
	} {
		if strings.Contains(out.Content, noise) {
			t.Errorf("应丢弃 %q, got:\n%s", noise, out.Content)
		}
	}
}

func TestPrismaFilter_Apply_Migrate(t *testing.T) {
	f := &PrismaFilter{}
	fixture := loadFixture(t, "prisma_migrate.txt")
	out := f.Apply(filter.FilterInput{
		Cmd:    "prisma",
		Args:   []string{"migrate", "dev", "--name", "add_users"},
		Stdout: fixture,
	})
	// 关键：migration 名和同步状态
	for _, want := range []string{
		"20260423091532_add_users",
		"database is now in sync",
		"Generated Prisma Client",
	} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留 %q, got:\n%s", want, out.Content)
		}
	}
	// 噪声
	for _, noise := range []string{
		"Environment variables",
		"Datasource \"db\"",
		"Tip: Get real-time",
	} {
		if strings.Contains(out.Content, noise) {
			t.Errorf("应丢弃 %q", noise)
		}
	}
}

// --- NextFilter ---

func TestNextFilter_Match(t *testing.T) {
	f := &NextFilter{}
	if !f.Match("next", []string{"build"}) {
		t.Error("应匹配 next build")
	}
	if f.Match("next", []string{"dev"}) {
		t.Error("不应匹配 next dev（长驻进程）")
	}
	if f.Match("next", []string{"start"}) {
		t.Error("不应匹配 next start（长驻进程）")
	}
}

func TestNextFilter_Apply(t *testing.T) {
	f := &NextFilter{}
	fixture := loadFixture(t, "next_build.txt")
	out := f.Apply(filter.FilterInput{
		Cmd:    "next",
		Args:   []string{"build"},
		Stdout: fixture,
	})
	// status checklist 和 Route 表保留
	for _, want := range []string{
		"Compiled successfully",
		"Generating static pages",
		"Route (app)",
		"First Load JS",
	} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留 %q, got:\n%s", want, out.Content)
		}
	}
	// 噪声丢
	for _, noise := range []string{
		"▲ Next.js",
		"Creating an optimized production build",
		"○  (Static)",
		"λ  (Dynamic)",
		"●  (SSG)",
	} {
		if strings.Contains(out.Content, noise) {
			t.Errorf("应丢弃 %q, got:\n%s", noise, out.Content)
		}
	}
}

func TestAllFilters_ApplyOnError_Nil(t *testing.T) {
	if out := (&PrismaFilter{}).ApplyOnError(filter.FilterInput{ExitCode: 1}); out != nil {
		t.Error("prisma ApplyOnError 应返回 nil")
	}
	if out := (&NextFilter{}).ApplyOnError(filter.FilterInput{ExitCode: 1}); out != nil {
		t.Error("next ApplyOnError 应返回 nil")
	}
}
