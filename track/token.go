package track

import "math"

// EstimateTokens 使用 ceil(chars/4) 近似估算 token 数
func EstimateTokens(text string) int {
	n := len([]rune(text))
	if n == 0 {
		return 0
	}
	return int(math.Ceil(float64(n) / 4.0))
}
