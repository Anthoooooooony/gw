package filter

// Registry 管理已注册的过滤器
type Registry struct {
	filters []Filter
}

// NewRegistry 创建空的过滤器注册表
func NewRegistry() *Registry {
	return &Registry{}
}

// Register 注册一个过滤器
func (r *Registry) Register(f Filter) {
	r.filters = append(r.filters, f)
}

// Find 查找匹配指定命令的第一个过滤器，未找到返回 nil
func (r *Registry) Find(cmd string, args []string) Filter {
	for _, f := range r.filters {
		if f.Match(cmd, args) {
			return f
		}
	}
	return nil
}

// DefaultRegistry 返回默认的过滤器注册表（当前为空）
func DefaultRegistry() *Registry {
	return NewRegistry()
}
