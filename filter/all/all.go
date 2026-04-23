// Package all 通过空白导入触发所有过滤器包的 init() 自注册。
// 匹配优先级由各 filter 自身声明（实现 filter.Fallback 的作为兜底），
// 此处 import 顺序仅控制 init() 运行次序，**不再承载语义不变式**。
package all

import (
	_ "github.com/Anthoooooooony/gw/filter/cargo"
	_ "github.com/Anthoooooooony/gw/filter/fs"
	_ "github.com/Anthoooooooony/gw/filter/git"
	_ "github.com/Anthoooooooony/gw/filter/java"
	_ "github.com/Anthoooooooony/gw/filter/net"
	_ "github.com/Anthoooooooony/gw/filter/npmtest"
	_ "github.com/Anthoooooooony/gw/filter/pip"
	_ "github.com/Anthoooooooony/gw/filter/pytest"
	_ "github.com/Anthoooooooony/gw/filter/toml"
)
