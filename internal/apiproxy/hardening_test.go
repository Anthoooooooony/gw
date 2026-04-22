package apiproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestMaxBodyBytes_RejectedOnContentLength 验证 Content-Length > 上限时立即 413。
// 注意：本测试不需要 upstream stub，因为 413 应在进入 rp.ServeHTTP 前就返回，
// 不会实际拨号。若未来 refactor 改动了 early-return 顺序，此测试会变成打真 Anthropic。
func TestMaxBodyBytes_RejectedOnContentLength(t *testing.T) {
	t.Setenv("GW_APIPROXY_MAX_BODY", "100") // 100 字节上限

	srv, err := Start(&testLogger{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Shutdown(2 * time.Second) }()

	// 构造一个明显超限的 body
	body := strings.Repeat("x", 1024)
	resp, err := http.Post(srv.URL()+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 413; body = %s", resp.StatusCode, b)
	}
}

// TestMaxBodyBytes_RejectedOnReadOverflow 验证 Content-Length 未知/欺骗时
// MaxBytesReader 在 ReadAll 时仍能兜底 413。
// 同 TestMaxBodyBytes_RejectedOnContentLength 的前提：无需 upstream stub。
func TestMaxBodyBytes_RejectedOnReadOverflow(t *testing.T) {
	t.Setenv("GW_APIPROXY_MAX_BODY", "100")

	srv, err := Start(&testLogger{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Shutdown(2 * time.Second) }()

	// 构造 chunked 请求让 ContentLength = -1
	req, _ := http.NewRequest(http.MethodPost, srv.URL()+"/v1/messages",
		strings.NewReader(strings.Repeat("x", 1024)))
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}
}

// TestMaxBodyBytes_DefaultAllowsNormalSize 验证默认上限下正常大小请求能通过。
// 关键：不设 GW_APIPROXY_MAX_BODY，默认 32MiB；发 100KB 必须通过。
func TestMaxBodyBytes_DefaultAllowsNormalSize(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	t.Setenv("GW_APIPROXY_UPSTREAM", upstream.URL)

	srv, err := Start(&testLogger{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Shutdown(2 * time.Second) }()

	body := `{"model":"x","max_tokens":1,"messages":[{"role":"user","content":"` +
		strings.Repeat("a", 100*1024) + `"}]}`
	resp, err := http.Post(srv.URL()+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// TestResponseHeaderTimeout 验证上游不返回响应头时代理回 502 而不是 hang。
func TestResponseHeaderTimeout(t *testing.T) {
	// 上游 handler 阻塞到测试清理，用 channel 替代 time.Sleep 避免真实墙钟等待。
	// defer 顺序很关键：close(block) 必须在 slow.Close 之前执行（LIFO：
	// 后 defer 先执行），否则 slow.Close() 会等待还在 <-block 上阻塞的 handler。
	block := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		w.WriteHeader(200)
	}))
	defer slow.Close()
	defer close(block)

	t.Setenv("GW_APIPROXY_UPSTREAM", slow.URL)
	t.Setenv("GW_APIPROXY_HEADER_TIMEOUT", "100ms")

	srv, err := Start(&testLogger{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Shutdown(2 * time.Second) }()

	start := time.Now()
	resp, err := http.Post(srv.URL()+"/v1/messages", "application/json", strings.NewReader("{}"))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
	// 应该远低于上游的 3s sleep
	if elapsed > 1*time.Second {
		t.Errorf("elapsed %v > 1s，说明 ResponseHeaderTimeout 未生效", elapsed)
	}
}

// TestShutdownTimeout_EnvOverride 验证 env 可覆盖默认 shutdown timeout。
func TestShutdownTimeout_EnvOverride(t *testing.T) {
	t.Setenv("GW_APIPROXY_SHUTDOWN_TIMEOUT", "250ms")
	got := ShutdownTimeout()
	want := 250 * time.Millisecond
	if got != want {
		t.Errorf("ShutdownTimeout() = %v, want %v", got, want)
	}
}

// TestShutdownTimeout_Default 验证未设 env 时返回默认值。
func TestShutdownTimeout_Default(t *testing.T) {
	t.Setenv("GW_APIPROXY_SHUTDOWN_TIMEOUT", "")
	if got := ShutdownTimeout(); got != defaultShutdownTimeout {
		t.Errorf("ShutdownTimeout() = %v, want %v", got, defaultShutdownTimeout)
	}
}

// TestShutdownTimeout_InvalidFallback 验证非法值回落默认。
func TestShutdownTimeout_InvalidFallback(t *testing.T) {
	t.Setenv("GW_APIPROXY_SHUTDOWN_TIMEOUT", "not-a-duration")
	if got := ShutdownTimeout(); got != defaultShutdownTimeout {
		t.Errorf("ShutdownTimeout() = %v, want %v", got, defaultShutdownTimeout)
	}
}
