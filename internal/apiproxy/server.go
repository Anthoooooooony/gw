// Package apiproxy 提供 Claude Code <-> Anthropic API 之间的本地透明 HTTP 代理。
//
// v0（PR1）：纯透传，无 DCP；仅用来验证 ANTHROPIC_BASE_URL 重定向链路。
// 后续 PR 在 RoundTrip 位置插入 tool_result 去重等上下文裁剪逻辑。
package apiproxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/Anthoooooooony/gw/internal/apiproxy/dcp"
)

// Server 持有 listener 与 http.Server 双份引用，便于在 Shutdown 时释放端口。
// Transformer 的引用也被 Server 持有，以便调用方通过 Stats() 读取 dcp 观测数据。
type Server struct {
	ln          net.Listener
	httpSrv     *http.Server
	addr        string
	transformer *dcp.Transformer
}

// Start 在 127.0.0.1 随机端口监听并后台启动 http.Server。
// 返回可供子进程使用的 URL（形如 "http://127.0.0.1:PORT"）。
func Start(logger Logger) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("apiproxy 监听失败: %w", err)
	}

	mux := http.NewServeMux()
	transformer := dcp.NewTransformer(logger)
	mux.HandleFunc("/v1/messages", anthropicHandler(logger, transformer.Transform))
	mux.HandleFunc("/v1/messages/count_tokens", anthropicHandler(logger, transformer.Transform))
	// health 端点便于外部 probe
	mux.HandleFunc("/_gw/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Warnf("apiproxy serve 异常: %v", err)
		}
	}()

	addr := "http://" + ln.Addr().String()
	logger.Infof("apiproxy 已监听 %s", addr)
	return &Server{ln: ln, httpSrv: srv, addr: addr, transformer: transformer}, nil
}

// URL 返回子进程应写入 ANTHROPIC_BASE_URL 的地址。
func (s *Server) URL() string { return s.addr }

// Stats 返回 dcp Transformer 的累积观测数据（非快照：继续被后续请求更新）。
func (s *Server) Stats() *dcp.Stats { return s.transformer.Stats() }

// Shutdown 优雅关闭；最长等 timeout 后强制终止。
func (s *Server) Shutdown(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.httpSrv.Shutdown(ctx)
}

// Logger 最小接口；实现侧可用 log.Printf 或任何 logger。
// 类型别名自 dcp.Logger，避免两处声明同形接口漂移：apiproxy 依赖 dcp，
// 复用 dcp 的声明不破坏 dcp 的独立可用性（dcp 是可单独使用的上下文压缩库）。
type Logger = dcp.Logger
