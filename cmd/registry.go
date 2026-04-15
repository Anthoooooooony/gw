package cmd

import (
	"github.com/gw-cli/gw/filter"
	filtergit "github.com/gw-cli/gw/filter/git"
	"github.com/gw-cli/gw/filter/java"
)

// buildRegistry 构建包含所有已注册过滤器的注册表
func buildRegistry() *filter.Registry {
	r := filter.NewRegistry()
	r.Register(&filtergit.StatusFilter{})
	r.Register(&filtergit.LogFilter{})
	r.Register(&java.MavenFilter{})
	r.Register(&java.GradleFilter{})
	r.Register(&java.SpringBootFilter{})
	return r
}
