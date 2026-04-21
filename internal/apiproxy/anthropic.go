package apiproxy

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// upstreamURL 返回 Anthropic 上游 API 的基础 URL。
// 可通过 GW_APIPROXY_UPSTREAM 覆盖（测试时把它指向 record/replay server）。
// 注意：本函数由 anthropicHandler 在 Start 时调用一次，结果在 handler 生命周期内固定。
// 测试里要生效必须在 Start() 之前设置 env；动态切换 upstream 不是设计目标。
func upstreamURL() *url.URL {
	raw := "https://api.anthropic.com"
	if v := envUpstream(); v != "" {
		raw = v
	}
	u, err := url.Parse(raw)
	if err != nil {
		// 理论不可达：硬编码默认值一定合法。
		panic("apiproxy: invalid upstream URL " + raw + ": " + err.Error())
	}
	return u
}

// BodyTransformer 在转发前对请求 body 做修改；nil 表示纯透传。
// 实现方应保证：失败降级为返回原 body（不抛错），任何内部异常由自己处理完。
type BodyTransformer func(body []byte) []byte

// upstreamTransport 构造上游 http.Transport。
// 显式设置 ResponseHeaderTimeout：若上游在该时限内未开始返回响应头，视为卡死并
// 让 ReverseProxy.ErrorHandler 回 502；该超时不影响 SSE 正文传输（正文可以任意长）。
//
// 其余字段沿用 http.DefaultTransport 的默认值（连接池、拨号超时、TLS 校验等）。
func upstreamTransport() http.RoundTripper {
	base, _ := http.DefaultTransport.(*http.Transport)
	if base == nil {
		// 理论不可达：DefaultTransport 永远是 *http.Transport。
		return http.DefaultTransport
	}
	clone := base.Clone()
	clone.ResponseHeaderTimeout = responseHeaderTimeout()
	return clone
}

// anthropicHandler 返回一个 reverse proxy，把请求原样转发到 api.anthropic.com。
// 当 transform 非 nil 时，POST 请求的 body 会在 handler 入口被读入、transform 后再
// 投给 ReverseProxy；这样 MaxBytes 超限可以干净返回 413，而不是污染响应流。
func anthropicHandler(logger Logger, transform BodyTransformer) http.HandlerFunc {
	target := upstreamURL()
	maxBytes := maxBodyBytes()
	rp := &httputil.ReverseProxy{
		Transport: upstreamTransport(),
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			// 去掉 Go http.Client 默认加上的 Accept-Encoding，避免上游返回压缩
			// 内容让中间态 SSE 解析复杂化。让 claude 直接拿原始字节。
			req.Header.Del("Accept-Encoding")
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// 上游异常时回 502 并把错误写回 body，便于 claude 端看到。
			logger.Warnf("apiproxy upstream error: %v", err)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "gw apiproxy: upstream error: "+err.Error())
		},
		// 透传 SSE：关闭 buffering，让 chunk 立即下发给 claude。
		FlushInterval: -1,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		// 去掉 Hop-by-hop 头，httputil.ReverseProxy 默认已处理 Connection，
		// 但某些客户端可能额外加 Proxy-Connection，保险起见清理一下。
		r.Header.Del("Proxy-Connection")
		// Claude Code 暂不走 WS/upgrade，遇到就 501 明确提示。
		if r.Header.Get("Upgrade") != "" {
			http.Error(w, "gw apiproxy: HTTP upgrade not supported", http.StatusNotImplemented)
			return
		}
		// Content-Length 已知时早拒大 body，省得 ReadAll 时才发现。
		if r.ContentLength > maxBytes {
			http.Error(w, "gw apiproxy: request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		// 仅对 POST 走 "读入-变换-注入" 路径；GET/HEAD 无 body 直接转发。
		if transform != nil && r.Method == http.MethodPost && r.Body != nil {
			// MaxBytesReader 超限返回 *http.MaxBytesError，让 handler 早 413 返回。
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			raw, err := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if err != nil {
				var mbe *http.MaxBytesError
				if errors.As(err, &mbe) {
					http.Error(w, "gw apiproxy: request body too large", http.StatusRequestEntityTooLarge)
					return
				}
				// 其他 read 错误：记录并让 ReverseProxy 通过 ErrorHandler 走 502。
				logger.Warnf("apiproxy: read body failed, returning 502: %v", err)
				http.Error(w, "gw apiproxy: read body failed", http.StatusBadGateway)
				return
			}
			out := transform(raw)
			// 替换 body 供 Director/ReverseProxy 使用
			r.Body = io.NopCloser(bytes.NewReader(out))
			r.ContentLength = int64(len(out))
		}
		rp.ServeHTTP(w, r)
	}
}
