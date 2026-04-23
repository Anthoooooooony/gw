package net

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

func TestWgetFilter_Match(t *testing.T) {
	f := &WgetFilter{}
	if !f.Match("wget", []string{"https://example.com"}) {
		t.Error("应匹配 wget")
	}
	if f.Match("curl", []string{"https://example.com"}) {
		t.Error("不应匹配 curl（输出结构不同）")
	}
}

func TestWgetFilter_Apply_Simple(t *testing.T) {
	f := &WgetFilter{}
	fixture := loadFixture(t, "wget_success.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "wget",
		Args:   []string{"https://example.com", "-O", "/tmp/x"},
		Stdout: fixture,
	})

	// URL / saved summary 关键行保留
	for _, want := range []string{"https://example.com/", "saved"} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留 %q, got:\n%s", want, out.Content)
		}
	}
	// 握手行应丢弃
	for _, noise := range []string{"Connecting to ", "Proxy request sent"} {
		if strings.Contains(out.Content, noise) {
			t.Errorf("应丢弃 %q, got:\n%s", noise, out.Content)
		}
	}
}

func TestWgetFilter_Apply_ProgressBars(t *testing.T) {
	f := &WgetFilter{}
	fixture := loadFixture(t, "wget_progress.txt")

	out := f.Apply(filter.FilterInput{
		Cmd:    "wget",
		Args:   []string{"https://example.com/large.bin"},
		Stdout: fixture,
	})

	// 进度条行（含 `NN%[==>  ]`）应全部丢弃
	for _, noise := range []string{
		"0%[",
		"10%[",
		"50%[",
		"80%[",
		"100%[",
	} {
		if strings.Contains(out.Content, noise) {
			t.Errorf("应丢弃进度条 %q, got:\n%s", noise, out.Content)
		}
	}
	// 关键信息保留
	for _, want := range []string{
		"https://example.com/large.bin",
		"Length: 10485760",
		"saved [10485760/10485760]",
	} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留 %q, got:\n%s", want, out.Content)
		}
	}
	// 应有显著压缩
	if len(out.Content) >= len(fixture) {
		t.Errorf("应产生压缩: got %d >= fixture %d", len(out.Content), len(fixture))
	}
}

func TestWgetFilter_Apply_HandshakeStripped(t *testing.T) {
	f := &WgetFilter{}
	input := "--2026-04-23--  https://x/\n" +
		"Resolving x (x)... 1.2.3.4\n" +
		"Connecting to x (x)|1.2.3.4|:443... connected.\n" +
		"HTTP request sent, awaiting response... 200 OK\n" +
		"Reusing existing connection to x:443.\n" +
		"Length: 100 [text/html]\n" +
		"Saving to: 'out'\n"
	out := f.Apply(filter.FilterInput{
		Cmd:    "wget",
		Args:   []string{"https://x/"},
		Stdout: input,
	})
	for _, noise := range []string{"Resolving ", "Connecting to ", "HTTP request", "Reusing existing"} {
		if strings.Contains(out.Content, noise) {
			t.Errorf("应丢弃 %q, got:\n%s", noise, out.Content)
		}
	}
	for _, want := range []string{"--2026-04-23--", "Length: 100", "Saving to:"} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("应保留 %q, got:\n%s", want, out.Content)
		}
	}
}

func TestWgetFilter_ApplyOnError(t *testing.T) {
	f := &WgetFilter{}
	if out := f.ApplyOnError(filter.FilterInput{ExitCode: 4}); out != nil {
		t.Errorf("ApplyOnError 应返回 nil, got %+v", out)
	}
}
