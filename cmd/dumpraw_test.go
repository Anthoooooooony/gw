package cmd

import (
	"reflect"
	"testing"
)

// TestExtractDumpRawFlag_HeadEquals --dump-raw=PATH 放在 args[0]
func TestExtractDumpRawFlag_HeadEquals(t *testing.T) {
	rest, path, found := extractDumpRawFlag([]string{"--dump-raw=/tmp/a", "git", "status"})
	if !found {
		t.Fatal("应找到 --dump-raw")
	}
	if path != "/tmp/a" {
		t.Errorf("path=%q, 期望 /tmp/a", path)
	}
	if !reflect.DeepEqual(rest, []string{"git", "status"}) {
		t.Errorf("rest=%v, 期望 [git status]", rest)
	}
}

// TestExtractDumpRawFlag_HeadSpace --dump-raw PATH 放在 args[0..1]
func TestExtractDumpRawFlag_HeadSpace(t *testing.T) {
	rest, path, found := extractDumpRawFlag([]string{"--dump-raw", "/tmp/b", "git", "status"})
	if !found {
		t.Fatal("应找到 --dump-raw")
	}
	if path != "/tmp/b" {
		t.Errorf("path=%q, 期望 /tmp/b", path)
	}
	if !reflect.DeepEqual(rest, []string{"git", "status"}) {
		t.Errorf("rest=%v, 期望 [git status]", rest)
	}
}

// TestExtractDumpRawFlag_MiddleBeforeCmd --dump-raw 出现在 args[1]
// 但仍在命令名之前（args[0] 是另一个未来可能支持的 gw exec flag）—
// 这里用 --verbose 模拟（当前不支持，但解析应扫描到首个非 -- 开头的 token 为止）
func TestExtractDumpRawFlag_MiddleBeforeCmd(t *testing.T) {
	// 模拟：--other-flag --dump-raw=/tmp/c echo hi
	rest, path, found := extractDumpRawFlag([]string{"--other-flag", "--dump-raw=/tmp/c", "echo", "hi"})
	if !found {
		t.Fatal("应找到 --dump-raw")
	}
	if path != "/tmp/c" {
		t.Errorf("path=%q, 期望 /tmp/c", path)
	}
	// --other-flag 保持原位
	if !reflect.DeepEqual(rest, []string{"--other-flag", "echo", "hi"}) {
		t.Errorf("rest=%v, 期望 [--other-flag echo hi]", rest)
	}
}

// TestExtractDumpRawFlag_MiddleSpace --dump-raw PATH 空格形式在中间
func TestExtractDumpRawFlag_MiddleSpace(t *testing.T) {
	rest, path, found := extractDumpRawFlag([]string{"--other", "--dump-raw", "/tmp/d", "echo", "hi"})
	if !found {
		t.Fatal("应找到 --dump-raw")
	}
	if path != "/tmp/d" {
		t.Errorf("path=%q, 期望 /tmp/d", path)
	}
	if !reflect.DeepEqual(rest, []string{"--other", "echo", "hi"}) {
		t.Errorf("rest=%v, 期望 [--other echo hi]", rest)
	}
}

// TestExtractDumpRawFlag_AfterCmd --dump-raw 出现在命令名之后应被忽略
// （避免误吞子命令的同名 flag）
func TestExtractDumpRawFlag_AfterCmd(t *testing.T) {
	args := []string{"my-tool", "--dump-raw=/tmp/inside", "arg2"}
	rest, path, found := extractDumpRawFlag(args)
	if found {
		t.Errorf("命令名之后的 --dump-raw 不应被处理，但 found=%v path=%q", found, path)
	}
	if !reflect.DeepEqual(rest, args) {
		t.Errorf("rest 应原样返回，得到 %v", rest)
	}
}

// TestExtractDumpRawFlag_None 无 --dump-raw
func TestExtractDumpRawFlag_None(t *testing.T) {
	args := []string{"git", "status"}
	rest, path, found := extractDumpRawFlag(args)
	if found {
		t.Error("不应找到 --dump-raw")
	}
	if path != "" {
		t.Errorf("path=%q, 期望空", path)
	}
	if !reflect.DeepEqual(rest, args) {
		t.Errorf("rest 应原样返回")
	}
}

// TestExtractDumpRawFlag_Empty 空 args
func TestExtractDumpRawFlag_Empty(t *testing.T) {
	rest, path, found := extractDumpRawFlag([]string{})
	if found {
		t.Error("空 args 不应找到 flag")
	}
	if path != "" || len(rest) != 0 {
		t.Errorf("rest=%v path=%q, 期望空", rest, path)
	}
}

// TestExtractDumpRawFlag_SpaceMissingValue --dump-raw 无后续 token
// 应视为未识别，原样返回（由后续命令执行报错）
func TestExtractDumpRawFlag_SpaceMissingValue(t *testing.T) {
	args := []string{"--dump-raw"}
	rest, path, found := extractDumpRawFlag(args)
	if found {
		t.Error("缺失值的 --dump-raw 不应识别为合法 flag")
	}
	if path != "" {
		t.Errorf("path 应为空，得到 %q", path)
	}
	if !reflect.DeepEqual(rest, args) {
		t.Errorf("rest 应原样返回")
	}
}
