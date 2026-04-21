package pytest

import (
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

func TestMatch(t *testing.T) {
	f := &Filter{}
	cases := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"pytest", nil, true},
		{"pytest", []string{"tests/"}, true},
		{"python", []string{"-m", "pytest"}, true},
		{"python3", []string{"-m", "pytest", "-v"}, true},
		{"python", []string{"-mpytest"}, true},
		{"python", []string{"-m", "unittest"}, false},
		{"pytest-cov", nil, false},
		{"tox", []string{"-e", "pytest"}, false}, // 自定义 wrapper 不认
	}
	for _, tc := range cases {
		got := f.Match(tc.cmd, tc.args)
		if got != tc.want {
			t.Errorf("Match(%q, %v) = %v, want %v", tc.cmd, tc.args, got, tc.want)
		}
	}
}

func TestApply_Success_KeepsOnlySummary(t *testing.T) {
	raw := strings.Join([]string{
		"============================= test session starts ==============================",
		"platform linux -- Python 3.11.7, pytest-8.0.0, pluggy-1.4.0",
		"cachedir: .pytest_cache",
		"rootdir: /workspace",
		"collected 42 items",
		"",
		"tests/test_api.py .......                                                 [100%]",
		"",
		"============================ 42 passed in 1.23s ===============================",
	}, "\n")

	f := &Filter{}
	out := f.Apply(filter.FilterInput{Cmd: "pytest", Stdout: raw})

	if !strings.Contains(out.Content, "42 passed in 1.23s") {
		t.Errorf("summary 行应保留，got=%q", out.Content)
	}
	// 成功场景 PASSED 进度行、平台诊断头都应丢弃
	for _, dropped := range []string{"platform linux", "cachedir:", "collected 42 items", "test_api.py"} {
		if strings.Contains(out.Content, dropped) {
			t.Errorf("成功场景输出不应含 %q，got=%q", dropped, out.Content)
		}
	}
}

func TestApply_NoSummary_FallbackToOriginal(t *testing.T) {
	// 用户用 `pytest | head -3` 截断了输出，找不到 summary → 保守透传原文
	raw := "tests/test_foo.py ....\ntests/test_bar.py ....\n"
	f := &Filter{}
	out := f.Apply(filter.FilterInput{Cmd: "pytest", Stdout: raw})
	if out.Content != raw {
		t.Errorf("找不到 summary 时应透传，got=%q want=%q", out.Content, raw)
	}
}

func TestApplyOnError_WithFailures_KeepsFailuresBlock(t *testing.T) {
	raw := strings.Join([]string{
		"============================= test session starts ==============================",
		"platform linux -- Python 3.11.7, pytest-8.0.0",
		"rootdir: /workspace",
		"collected 3 items",
		"",
		"tests/test_a.py .F.                                                       [100%]",
		"",
		"=================================== FAILURES ===================================",
		"_____________________________ test_user_create _________________________________",
		"    def test_user_create():",
		">       assert User.objects.count() == 1",
		"E       AssertionError: assert 0 == 1",
		"tests/test_a.py:5: AssertionError",
		"=========================== short test summary info ============================",
		"FAILED tests/test_a.py::test_user_create - AssertionError",
		"========================= 1 failed, 2 passed in 0.34s ==========================",
	}, "\n")

	f := &Filter{}
	out := f.ApplyOnError(filter.FilterInput{Cmd: "pytest", Stdout: raw, ExitCode: 1})
	if out == nil {
		t.Fatal("有 FAILURES + summary 时应返回非 nil")
	}
	// 关键信息必须保留
	for _, key := range []string{
		"FAILURES",
		"test_user_create",
		"AssertionError: assert 0 == 1",
		"tests/test_a.py:5",
		"short test summary info",
		"FAILED tests/test_a.py::test_user_create",
		"1 failed, 2 passed in 0.34s",
	} {
		if !strings.Contains(out.Content, key) {
			t.Errorf("关键信息 %q 应保留，got=%q", key, out.Content)
		}
	}
	// 诊断头应被丢弃
	for _, dropped := range []string{"platform linux", "rootdir:", "collected 3 items", "test_a.py .F."} {
		if strings.Contains(out.Content, dropped) {
			t.Errorf("诊断头 %q 不应保留，got=%q", dropped, out.Content)
		}
	}
}

func TestApplyOnError_NoFailuresHeader_ReturnsNil(t *testing.T) {
	// pytest 失败但用户用 `--tb=no` 抑制了 FAILURES 节；保守返回 nil 让上层透传
	raw := strings.Join([]string{
		"============================= test session starts ==============================",
		"collected 1 item",
		"tests/test_a.py F                                                        [100%]",
		"========================= 1 failed in 0.05s =====================================",
	}, "\n")

	f := &Filter{}
	out := f.ApplyOnError(filter.FilterInput{Cmd: "pytest", Stdout: raw, ExitCode: 1})
	if out != nil {
		t.Errorf("无 FAILURES 节时应返回 nil，got=%+v", out)
	}
}

func TestApplyOnError_NoSummary_ReturnsNil(t *testing.T) {
	// 输出被截断，找不到 summary → nil 透传
	raw := "tests/test_a.py F\n=== FAILURES ===\nincomplete..."
	f := &Filter{}
	out := f.ApplyOnError(filter.FilterInput{Cmd: "pytest", Stdout: raw, ExitCode: 1})
	if out != nil {
		t.Errorf("无 summary 时应返回 nil，got=%+v", out)
	}
}

func TestSubname(t *testing.T) {
	f := &Filter{}
	if got := f.Subname("pytest", nil); got != "pytest" {
		t.Errorf("Subname(pytest) = %q, want pytest", got)
	}
	if got := f.Subname("python", []string{"-m", "pytest"}); got != "python -m pytest" {
		t.Errorf("Subname(python -m pytest) = %q, want 'python -m pytest'", got)
	}
	// 编译期 assert SubnameResolver 实现
	var _ filter.SubnameResolver = f
}

func TestFinalSummaryRe(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"============================ 42 passed in 1.23s ===============================", true},
		{"========================= 1 failed, 2 passed in 0.34s ==========================", true},
		{"===== 1 failed, 1 passed, 2 skipped in 3.21s ====", true},
		{"= 99 passed in 0.08s =", true},
		{"============================= test session starts ==============================", false},
		{"=================================== FAILURES ===================================", false},
		{"=========================== short test summary info ============================", false},
		{"tests/test_a.py ....", false},
	}
	for _, tc := range cases {
		got := finalSummaryRe.MatchString(tc.line)
		if got != tc.want {
			t.Errorf("finalSummaryRe(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}
