package java

// boundedDedupSet 是容量受限的去重集合：超出 capacity 后停止吸收新键。
// 长驻流式构建（Gradle/Maven）若遇到高基数输出（如逐行异常堆栈）可能会把
// 无限增长的 map 推到 100MB 级别，这里用硬上限换有损但稳定的去重。
//
// 容量内行为与普通 map 一致；达到上限后 Add 返回 false，调用方退化为"不去重、
// 直接 emit"的保守路径，比 OOM 更友好。
type boundedDedupSet struct {
	m   map[string]struct{}
	cap int
}

// newBoundedDedupSet 构造容量为 cap 的集合。cap <= 0 视作无限（但调用方应避免这样用）。
func newBoundedDedupSet(cap int) *boundedDedupSet {
	return &boundedDedupSet{m: make(map[string]struct{}), cap: cap}
}

// Has 返回 key 是否已被记录过。
func (s *boundedDedupSet) Has(key string) bool {
	_, ok := s.m[key]
	return ok
}

// Add 尝试把 key 加入集合。返回 true 表示新加入，false 表示已存在或容量已满。
// 容量满后 Add 不再吸收新键；Has 仍在已记录集合上正确返回。
func (s *boundedDedupSet) Add(key string) bool {
	if _, ok := s.m[key]; ok {
		return false
	}
	if s.cap > 0 && len(s.m) >= s.cap {
		return false
	}
	s.m[key] = struct{}{}
	return true
}
