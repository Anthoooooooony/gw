package apiproxy

import (
	"os"
	"strconv"
	"time"
)

// envUpstream 读取测试逃生舱：GW_APIPROXY_UPSTREAM 可把上游指向任意 URL。
// 未设置时返回空串，调用方使用默认 https://api.anthropic.com。
func envUpstream() string {
	return os.Getenv("GW_APIPROXY_UPSTREAM")
}

// 默认 hardening 参数。Claude Code 上下文窗口 200K tokens ≈ 1MB，32MB 留出 30x 余量。
const (
	defaultMaxBodyBytes          int64         = 32 << 20 // 32 MiB
	defaultResponseHeaderTimeout time.Duration = 60 * time.Second
	defaultShutdownTimeout       time.Duration = 5 * time.Second
)

// maxBodyBytes 允许通过 GW_APIPROXY_MAX_BODY 以字节数覆盖。解析失败回落默认值。
func maxBodyBytes() int64 {
	v := os.Getenv("GW_APIPROXY_MAX_BODY")
	if v == "" {
		return defaultMaxBodyBytes
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return defaultMaxBodyBytes
	}
	return n
}

// responseHeaderTimeout 允许通过 GW_APIPROXY_HEADER_TIMEOUT 以 Go duration 字符串覆盖。
// 不影响 SSE 响应正文——只约束上游"开始返回响应头"的最长等待时间。
func responseHeaderTimeout() time.Duration {
	v := os.Getenv("GW_APIPROXY_HEADER_TIMEOUT")
	if v == "" {
		return defaultResponseHeaderTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return defaultResponseHeaderTimeout
	}
	return d
}

// ShutdownTimeout 允许通过 GW_APIPROXY_SHUTDOWN_TIMEOUT 以 Go duration 字符串覆盖。
// 暴露为 Exported 以便 gw claude 在 Server.Shutdown 调用时使用同一来源。
func ShutdownTimeout() time.Duration {
	v := os.Getenv("GW_APIPROXY_SHUTDOWN_TIMEOUT")
	if v == "" {
		return defaultShutdownTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return defaultShutdownTimeout
	}
	return d
}
