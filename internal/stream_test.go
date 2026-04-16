package internal

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunCommandStreaming_MultiLine(t *testing.T) {
	var lines []string
	code, err := RunCommandStreaming("printf", []string{"line1\nline2\nline3\n"}, func(line string) {
		lines = append(lines, line)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	expected := []string{"line1", "line2", "line3"}
	for i, want := range expected {
		if lines[i] != want {
			t.Errorf("line[%d]: expected %q, got %q", i, want, lines[i])
		}
	}
}

func TestRunCommandStreaming_ExitCode(t *testing.T) {
	var lines []string
	code, err := RunCommandStreaming("sh", []string{"-c", "echo hello; exit 42"}, func(line string) {
		lines = append(lines, line)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 42 {
		t.Fatalf("expected exit code 42, got %d", code)
	}
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("expected [\"hello\"], got %v", lines)
	}
}

func TestRunCommandStreamingFull_Stderr(t *testing.T) {
	var lines []string
	var stderrBuf bytes.Buffer
	code, err := RunCommandStreamingFull("sh", []string{"-c", "echo out; echo err >&2"}, func(line string) {
		lines = append(lines, line)
	}, &stderrBuf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if len(lines) != 1 || lines[0] != "out" {
		t.Fatalf("expected stdout [\"out\"], got %v", lines)
	}
	stderrOutput := strings.TrimSpace(stderrBuf.String())
	if stderrOutput != "err" {
		t.Fatalf("expected stderr \"err\", got %q", stderrOutput)
	}
}

func TestRunCommandStreaming_NotFound(t *testing.T) {
	_, err := RunCommandStreaming("nonexistent_command_xyz_12345", nil, func(line string) {})
	if err == nil {
		t.Fatal("expected error for nonexistent command, got nil")
	}
}
