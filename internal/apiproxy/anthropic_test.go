package apiproxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testLogger 实现 Logger 接口，捕获输出供断言。
type testLogger struct{ buf bytes.Buffer }

func (l *testLogger) Infof(f string, a ...any) { l.buf.WriteString("I:"); l.buf.WriteString(f) }
func (l *testLogger) Warnf(f string, a ...any) { l.buf.WriteString("W:"); l.buf.WriteString(f) }

// TestPassthrough_Body 验证请求 body 和响应 body 原样透传，header 也保留。
func TestPassthrough_Body(t *testing.T) {
	var gotHeaders http.Header
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("anthropic-request-id", "req_fake_123")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"msg_01","type":"message","role":"assistant"}`))
	}))
	defer upstream.Close()

	t.Setenv("GW_APIPROXY_UPSTREAM", upstream.URL)

	srv, err := Start(&testLogger{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Shutdown(2 * time.Second) }()

	reqBody := `{"model":"claude-sonnet-4","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`
	req, _ := http.NewRequest("POST", srv.URL()+"/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("x-api-key", "sk-test")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("X-Claude-Code-Session-Id", "sess_abc")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("anthropic-request-id"); got != "req_fake_123" {
		t.Errorf("response header lost: got %q", got)
	}
	gotResp, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(gotResp), "msg_01") {
		t.Errorf("response body corrupted: %s", gotResp)
	}

	// 关键 header 必须原样到达上游
	if got := gotHeaders.Get("X-Api-Key"); got != "sk-test" {
		t.Errorf("x-api-key lost: got %q", got)
	}
	if got := gotHeaders.Get("Anthropic-Version"); got != "2023-06-01" {
		t.Errorf("anthropic-version lost: got %q", got)
	}
	if got := gotHeaders.Get("X-Claude-Code-Session-Id"); got != "sess_abc" {
		t.Errorf("X-Claude-Code-Session-Id lost: got %q", got)
	}
	if string(gotBody) != reqBody {
		t.Errorf("request body corrupted: got %s", gotBody)
	}
}

// TestPassthrough_SSE 验证 SSE 流式响应被逐 chunk 下发（FlushInterval=-1）。
func TestPassthrough_SSE(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		for _, chunk := range []string{
			"event: message_start\ndata: {\"type\":\"message_start\"}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hi\"}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		} {
			_, _ = io.WriteString(w, chunk)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer upstream.Close()

	t.Setenv("GW_APIPROXY_UPSTREAM", upstream.URL)

	srv, err := Start(&testLogger{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Shutdown(2 * time.Second) }()

	resp, err := http.Post(srv.URL()+"/v1/messages", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"message_start", "content_block_delta", "message_stop"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("SSE chunk %q missing from body", want)
		}
	}
}

// TestUpstreamError 验证上游不可达时代理回 502 而非卡死或 500。
func TestUpstreamError(t *testing.T) {
	// 指向一个不监听的端口
	t.Setenv("GW_APIPROXY_UPSTREAM", "http://127.0.0.1:1")

	srv, err := Start(&testLogger{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Shutdown(2 * time.Second) }()

	resp, err := http.Post(srv.URL()+"/v1/messages", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}

// TestHealth 验证 _gw/health 端点可用于外部 probe。
func TestHealth(t *testing.T) {
	srv, err := Start(&testLogger{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Shutdown(2 * time.Second) }()

	resp, err := http.Get(srv.URL() + "/_gw/health")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Errorf("health status = %d", resp.StatusCode)
	}
}
