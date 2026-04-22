package filter

import "regexp"

// ansiRe 匹配 ANSI CSI 转义序列（终端颜色 / 位置控制字符），足够覆盖大部分
// 构建工具输出（mvn/gradle/pytest/cargo 等），并不追求覆盖全部 ANSI 标准。
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI 去除字符串中的 ANSI CSI 转义序列，供各 filter / toml 引擎公用。
// 幂等：对不含转义的字符串返回原串的副本。
func StripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}
