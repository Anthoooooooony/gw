package apiproxy

import (
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

// anthropicHandler 返回一个 reverse proxy，把请求原样转发到 api.anthropic.com。
// v0 不做任何 body 改写，只保证 header/body/stream 透传正确。
func anthropicHandler(logger Logger) http.HandlerFunc {
	target := upstreamURL()
	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			// 去掉 Go http.Client 默认加上的 Accept-Encoding，避免上游返回压缩
			// 内容让中间态 SSE 解析复杂化。v0 让 claude 直接拿原始字节。
			req.Header.Del("Accept-Encoding")
		},
		// v0 不改 body；ModifyResponse 留空，上游响应原样透传（包括 SSE）。
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
		rp.ServeHTTP(w, r)
	}
}
