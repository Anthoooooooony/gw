package filter

// global 是通过 init() 自注册填充的全局过滤器注册表
var global = &Registry{}

// Register 将过滤器注册到全局注册表（由各过滤器包的 init() 调用）
func Register(f Filter) {
	global.filters = append(global.filters, f)
}

// GlobalRegistry 返回全局过滤器注册表
func GlobalRegistry() *Registry {
	return global
}

// Registry 管理已注册的过滤器
type Registry struct {
	filters []Filter
}

// NewRegistry 创建空的过滤器注册表
func NewRegistry() *Registry {
	return &Registry{}
}

// Add 注册一个过滤器到注册表实例
func (r *Registry) Add(f Filter) {
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

// FindStream 查找匹配的 StreamFilter
func (r *Registry) FindStream(cmd string, args []string) StreamFilter {
	f := r.Find(cmd, args)
	if f == nil {
		return nil
	}
	if sf, ok := f.(StreamFilter); ok {
		return sf
	}
	return nil
}

// FindStream 在全局注册表中查找匹配的 StreamFilter
func FindStream(cmd string, args []string) StreamFilter {
	return global.FindStream(cmd, args)
}
