package filter

// FilterInput 是过滤器的输入，包含命令执行的完整结果
type FilterInput struct {
	Cmd      string
	Args     []string
	Stdout   string
	Stderr   string
	ExitCode int
}

// FilterOutput 是过滤器的输出
type FilterOutput struct {
	Content  string // 过滤后的内容
	Original string // 原始内容
}

// Filter 定义了命令输出过滤器的接口
type Filter interface {
	// Name 返回过滤器的**静态**名称（不依赖当前匹配的命令，所有调用返回同一值）。
	// 子规则名（例如 TomlFilter 匹配到的 rule.Match）通过 SubnameResolver 单独暴露。
	Name() string
	// Match 判断当前过滤器是否匹配该命令。**不应持有"本次匹配"的副作用**——
	// 否则在并发或多次调用下会互相覆盖状态。
	Match(cmd string, args []string) bool
	// Apply 在命令成功执行时(exit==0)应用过滤
	Apply(input FilterInput) FilterOutput
	// ApplyOnError 在命令执行失败时(exit!=0)应用过滤，返回 nil 表示直接透传原始输出
	ApplyOnError(input FilterInput) *FilterOutput
}

// SubnameResolver 是可选接口：实现后 Registry.Find 在匹配命中时顺便解析出本次匹配的
// "子名"（如 TomlFilter 里的 rule.Match）。把子名作为**纯函数**从 (cmd, args) 推出，
// 而不是放到 filter 实例字段里——避免共享可变状态带来的 race / TOCTOU 脆弱性。
type SubnameResolver interface {
	Subname(cmd string, args []string) string
}

// Fallback 是可选接口：实现并返回 true 的 filter 在 Registry.Find 中作为兜底，
// 即**所有非兜底 filter 未命中后**才考虑它。典型用例是 TOML 声明式规则——
// 专属 Go filter 语义更精确，应当优先匹配。
//
// 这样匹配优先级由 filter 自身声明，而不是依赖 filter/all 的 import 顺序，
// 后者是隐式不变式，一旦有新包插入中间就可能默默破坏。
type Fallback interface {
	IsFallback() bool
}

// FilterRow 是 `gw filters list` 表格的一行。
type FilterRow struct {
	Name   string // 展示名（TOML 里是 rule ID，Go 里是 Filter.Name()）
	Type   string // go | toml
	Source string // builtin | user://... | project://...
	Match  string // 命令匹配模式
}

// Describable 是可选接口：实现后 `gw filters list` 用 Describe 生成表格行，
// 而不是让 cmd 包对具体 filter 类型做 type assertion。TOML 过滤器承载多条规则，
// 需要展开为多行——是该接口的主要用例。
type Describable interface {
	Describe() []FilterRow
}

// Match 是 Registry.Find 的返回值：匹配到的 filter 和（可选）本次匹配的子名。
// 展示用的 FilterUsed 拼接为 "<Filter.Name>/<Subname>"；Subname 为空时只用 Filter.Name。
type Match struct {
	Filter  Filter
	Subname string
}

// StreamAction 表示流式过滤中对单行的决策
type StreamAction int

const (
	StreamDrop StreamAction = iota // 丢弃此行
	StreamEmit                     // 立即输出此行
)

// StreamFilter 是支持流式（逐行）过滤的接口。
// 有状态设计：每次命令执行调用 NewStreamInstance() 创建新处理器实例。
type StreamFilter interface {
	Filter
	NewStreamInstance() StreamProcessor
}

// StreamProcessor 是单次命令执行的流式处理器，持有本次执行的状态。
type StreamProcessor interface {
	ProcessLine(line string) (action StreamAction, output string)
	Flush(exitCode int) []string
}
