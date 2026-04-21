package apiproxy

import "os"

// envUpstream 读取测试逃生舱：GW_APIPROXY_UPSTREAM 可把上游指向任意 URL。
// 未设置时返回空串，调用方使用默认 https://api.anthropic.com。
func envUpstream() string {
	return os.Getenv("GW_APIPROXY_UPSTREAM")
}

// BedrockOrVertexEnabled 检测 Claude Code 的 Bedrock/Vertex 开关。
// 这两条路径下 ANTHROPIC_BASE_URL 失效，gw claude 不应启动代理。
func BedrockOrVertexEnabled() (enabled bool, which string) {
	if os.Getenv("CLAUDE_CODE_USE_BEDROCK") == "1" {
		return true, "CLAUDE_CODE_USE_BEDROCK"
	}
	if os.Getenv("CLAUDE_CODE_USE_VERTEX") == "1" {
		return true, "CLAUDE_CODE_USE_VERTEX"
	}
	return false, ""
}
