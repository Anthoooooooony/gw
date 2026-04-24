package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Anthoooooooony/gw/internal/apiproxy/dedup"
)

func TestStderrLogger_VerboseOn_WritesInfoAndWarn(t *testing.T) {
	var buf bytes.Buffer
	l := newStderrLogger(&buf, true)

	l.Infof("hello %d", 42)
	l.Warnf("uh oh: %s", "x")

	out := buf.String()
	if !strings.Contains(out, "gw: info: hello 42") {
		t.Errorf("verbose 模式应打印 info 行，got=%q", out)
	}
	if !strings.Contains(out, "gw: warning: uh oh: x") {
		t.Errorf("应打印 warn 行，got=%q", out)
	}
}

func TestStderrLogger_VerboseOff_SuppressesInfo(t *testing.T) {
	var buf bytes.Buffer
	l := newStderrLogger(&buf, false)

	l.Infof("should not appear")
	l.Warnf("warning always appears")

	out := buf.String()
	if strings.Contains(out, "should not appear") {
		t.Errorf("非 verbose 下 info 不应打印，got=%q", out)
	}
	if !strings.Contains(out, "gw: warning: warning always appears") {
		t.Errorf("warning 应始终打印，got=%q", out)
	}
}

func TestWriteDedupSummary_SkipsWhenNoRequests(t *testing.T) {
	var buf bytes.Buffer
	var stats dedup.Stats
	writeDedupSummary(&buf, &stats)
	if buf.Len() != 0 {
		t.Errorf("无请求时不应产生摘要，got=%q", buf.String())
	}
}

func TestWriteDedupSummary_EmitsStats(t *testing.T) {
	var buf bytes.Buffer
	var stats dedup.Stats
	stats.RequestsProcessed.Add(3)
	stats.ToolUseScanned.Add(10)
	stats.ResultsReplaced.Add(4)
	stats.BytesInput.Add(2000)
	stats.BytesOutput.Add(1500)

	writeDedupSummary(&buf, &stats)

	out := buf.String()
	for _, want := range []string{
		"gw: dedup:",
		"3 请求",
		"扫 10 tool_use",
		"替换 4 tool_result",
		"节省 500 字节",
		"tokens",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("摘要应包含 %q，got=%q", want, out)
		}
	}
}
