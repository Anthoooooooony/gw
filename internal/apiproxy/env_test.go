package apiproxy

import "testing"

func TestBedrockOrVertexEnabled(t *testing.T) {
	tests := []struct {
		name      string
		bedrock   string
		vertex    string
		wantOK    bool
		wantWhich string
	}{
		{"neither", "", "", false, ""},
		{"bedrock 1", "1", "", true, "CLAUDE_CODE_USE_BEDROCK"},
		{"vertex 1", "", "1", true, "CLAUDE_CODE_USE_VERTEX"},
		{"bedrock wins when both", "1", "1", true, "CLAUDE_CODE_USE_BEDROCK"},
		{"bedrock 0 not enabled", "0", "", false, ""},
		{"bedrock true not enabled (只认 1)", "true", "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLAUDE_CODE_USE_BEDROCK", tt.bedrock)
			t.Setenv("CLAUDE_CODE_USE_VERTEX", tt.vertex)
			ok, which := BedrockOrVertexEnabled()
			if ok != tt.wantOK || which != tt.wantWhich {
				t.Errorf("got (%v, %q), want (%v, %q)", ok, which, tt.wantOK, tt.wantWhich)
			}
		})
	}
}
