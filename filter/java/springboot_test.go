package java

import (
	"strings"
	"testing"

	"github.com/gw-cli/gw/filter"
)

func TestSpringBootFilter_Match(t *testing.T) {
	f := &SpringBootFilter{}

	tests := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"java", []string{"-jar", "app.jar"}, true},
		{"java", []string{"-jar", "demo-1.0.0-SNAPSHOT.jar"}, true},
		{"java", []string{"-Xmx512m", "-jar", "service.jar"}, true},
		{"java", []string{"-version"}, false},
		{"java", []string{"com.example.Main"}, false},
		{"mvn", []string{"spring-boot:run"}, false},
		{"gradle", []string{"bootRun"}, false},
	}

	for _, tt := range tests {
		got := f.Match(tt.cmd, tt.args)
		if got != tt.want {
			t.Errorf("Match(%q, %v) = %v, want %v", tt.cmd, tt.args, got, tt.want)
		}
	}
}

func TestSpringBootFilter_Apply(t *testing.T) {
	f := &SpringBootFilter{}
	fixture := loadFixture(t, "springboot_startup.txt")

	output := f.Apply(filter.FilterInput{
		Cmd:      "java",
		Args:     []string{"-jar", "demo.jar"},
		Stdout:   fixture,
		ExitCode: 0,
	})

	// 应保留端口信息
	if !strings.Contains(output.Content, "port 8080") {
		t.Error("应该保留端口信息")
	}

	// 应保留 Started 消息
	if !strings.Contains(output.Content, "Started DemoApplication") {
		t.Error("应该保留 Started 消息")
	}

	// 应保留 WARN 消息
	if !strings.Contains(output.Content, "WARN") {
		t.Error("应该保留 WARN 消息")
	}

	// 不应包含 ASCII banner
	if strings.Contains(output.Content, "____") {
		t.Error("不应包含 ASCII banner")
	}
	if strings.Contains(output.Content, ":: Spring Boot ::") {
		t.Error("不应包含 Spring Boot banner 标记")
	}

	// 不应包含 Hibernate HHH000 日志
	if strings.Contains(output.Content, "HHH0") {
		t.Error("不应包含 Hibernate HHH000 日志")
	}

	// 不应包含 Tomcat 引擎内部
	if strings.Contains(output.Content, "catalina.core.Standard") {
		t.Error("不应包含 Tomcat 引擎内部信息")
	}

	// 不应包含 Spring Data 扫描详情
	if strings.Contains(output.Content, "RepositoryConfigurationDelegate") {
		t.Error("不应包含 Spring Data 扫描详情")
	}

	// 不应包含 profile fallback
	if strings.Contains(output.Content, "falling back to") {
		t.Error("不应包含 profile fallback 行")
	}
}

func TestSpringBootFilter_ApplyOnError(t *testing.T) {
	f := &SpringBootFilter{}
	result := f.ApplyOnError(filter.FilterInput{
		Cmd:      "java",
		Args:     []string{"-jar", "app.jar"},
		ExitCode: 1,
	})
	if result != nil {
		t.Error("ApplyOnError 应该返回 nil（启动失败需要完整栈追踪）")
	}
}
