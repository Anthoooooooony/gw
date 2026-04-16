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
	// Name 返回过滤器的描述性名称
	Name() string
	// Match 判断当前过滤器是否匹配该命令
	Match(cmd string, args []string) bool
	// Apply 在命令成功执行时(exit==0)应用过滤
	Apply(input FilterInput) FilterOutput
	// ApplyOnError 在命令执行失败时(exit!=0)应用过滤，返回 nil 表示直接透传原始输出
	ApplyOnError(input FilterInput) *FilterOutput
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
