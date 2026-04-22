package filter

import "sync"

// global 是通过 init() 自注册填充的全局过滤器注册表
var global = &Registry{}

// Register 将过滤器注册到全局注册表（由各过滤器包的 init() 调用）
func Register(f Filter) {
	global.Add(f)
}

// GlobalRegistry 返回全局过滤器注册表
func GlobalRegistry() *Registry {
	return global
}

// Registry 管理已注册的过滤器。
// 加锁后并发安全：init() 注册 + 运行期读取 + 未来可能的热加载均不会 race。
type Registry struct {
	mu      sync.RWMutex
	filters []Filter
}

// NewRegistry 创建空的过滤器注册表
func NewRegistry() *Registry {
	return &Registry{}
}

// Add 注册一个过滤器到注册表实例
func (r *Registry) Add(f Filter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.filters = append(r.filters, f)
}

// List 返回注册表中所有过滤器的快照（副本，遍历期间修改注册表不受影响）
func (r *Registry) List() []Filter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Filter, len(r.filters))
	copy(out, r.filters)
	return out
}

// Find 查找匹配指定命令的第一个过滤器，未找到返回 nil。
// 匹配顺序：先扫非 Fallback 过滤器，再扫 Fallback 过滤器（TOML 规则天然是兜底）。
// 这样即使 init() 导入顺序变化，专属 Go 过滤器始终优先于 TOML 通配规则。
// 匹配的 filter 若实现 SubnameResolver，用 Subname(cmd, args) 解析本次匹配的子名。
func (r *Registry) Find(cmd string, args []string) *Match {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if m := findIn(r.filters, cmd, args, false); m != nil {
		return m
	}
	return findIn(r.filters, cmd, args, true)
}

func findIn(filters []Filter, cmd string, args []string, wantFallback bool) *Match {
	for _, f := range filters {
		if isFallback(f) != wantFallback {
			continue
		}
		if f.Match(cmd, args) {
			m := &Match{Filter: f}
			if sr, ok := f.(SubnameResolver); ok {
				m.Subname = sr.Subname(cmd, args)
			}
			return m
		}
	}
	return nil
}

func isFallback(f Filter) bool {
	fb, ok := f.(Fallback)
	return ok && fb.IsFallback()
}

// FindStream 查找匹配的 StreamFilter
func (r *Registry) FindStream(cmd string, args []string) StreamFilter {
	m := r.Find(cmd, args)
	if m == nil {
		return nil
	}
	if sf, ok := m.Filter.(StreamFilter); ok {
		return sf
	}
	return nil
}

// FindStream 在全局注册表中查找匹配的 StreamFilter
func FindStream(cmd string, args []string) StreamFilter {
	return global.FindStream(cmd, args)
}
