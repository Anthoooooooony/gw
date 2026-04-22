// Package all 通过空白导入触发所有过滤器包的 init() 自注册
package all

import (
	_ "github.com/Anthoooooooony/gw/filter/git"
	_ "github.com/Anthoooooooony/gw/filter/java"
	_ "github.com/Anthoooooooony/gw/filter/pytest"
	// toml 放最后：专属 filter 优先（Registry.Find 是第一匹配胜出）
	_ "github.com/Anthoooooooony/gw/filter/toml"
)
