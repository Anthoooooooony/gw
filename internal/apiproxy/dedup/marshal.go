package dedup

import (
	"bytes"
	"encoding/json"
)

// marshalNoEscape 等价于 json.Marshal，但关闭默认的 HTML escape。
//
// 标准库 json.Marshal 默认会把 '<' '>' '&' 替换成 6 字节的 \uXXXX 转义
// （各 +5 字节），这是为了让 JSON 可以安全嵌入 HTML 上下文。Anthropic API
// 不做 HTML 解析，这条逃逸对我们是纯 cost：
//  1. 膨胀 request body；
//  2. 污染 Anthropic prompt cache 的字节键；
//  3. 让 dedup 替换小于占位符长度时，膨胀效应把节省吞没，BytesSaved 显示 0。
//
// json.Encoder.SetEscapeHTML(false) 会把"不 escape"一路传到 appendCompact，
// 对嵌套的 Marshaler / RawMessage 同样生效。
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	// Encoder.Encode 总会追加一个 '\n'，去掉与 json.Marshal 对齐。
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	return out, nil
}
