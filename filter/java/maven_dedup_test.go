package java

import "testing"

func TestExtractErrorKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Unresolved reference
		{"file:///app/Foo.kt:8:52 Unresolved reference 'BusinessLog'.", "unresolved:'BusinessLog'."},
		// Type mismatch
		{"Type mismatch: inferred type is String but Int was expected", "type_mismatch:: inferred type is String but Int was expected"},
		// Cannot access class
		{"Cannot access class 'com.example.Internal': it is internal in 'com.example'", "access:'com.example.Internal': it is internal in 'com.example'"},
		// No match
		{"Failed to execute goal org.apache.maven.plugins:maven-compiler-plugin", ""},
		// Empty string
		{"", ""},
	}
	for _, tt := range tests {
		got := extractErrorKey(tt.input)
		if got != tt.want {
			t.Errorf("extractErrorKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
