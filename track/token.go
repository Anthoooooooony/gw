package track

import "math"

// EstimateTokens 使用 ceil(chars/4) 近似估算 token 数
func EstimateTokens(text string) int {
	return EstimateTokensByLen(len([]rune(text)))
}

// EstimateTokensByLen 按字符数估算 token 数，避免分配字符串
func EstimateTokensByLen(charCount int) int {
	if charCount == 0 {
		return 0
	}
	return int(math.Ceil(float64(charCount) / 4.0))
}
