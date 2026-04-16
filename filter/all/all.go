// Package all 通过空白导入触发所有过滤器包的 init() 自注册
package all

import (
	_ "github.com/gw-cli/gw/filter/git"
	_ "github.com/gw-cli/gw/filter/java"
	_ "github.com/gw-cli/gw/filter/toml"
)
