package cmd

// 通过空白导入触发所有过滤器的 init() 自注册
import _ "github.com/gw-cli/gw/filter/all"
