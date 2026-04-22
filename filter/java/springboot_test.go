package java

import (
	"strings"
	"testing"

	"github.com/Anthoooooooony/gw/filter"
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
	if !strings.Contains(output.Content, "Started PetClinicApplication") {
		t.Error("应该保留 Started 消息")
	}

	// 不应包含 ASCII banner
	if strings.Contains(output.Content, "____") {
		t.Error("不应包含 ASCII banner")
	}
	if strings.Contains(output.Content, ":: Spring Boot ::") || strings.Contains(output.Content, ":: Built with Spring Boot ::") {
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

	// 不应包含 HikariPool
	if strings.Contains(output.Content, "HikariPool") {
		t.Error("不应包含 HikariPool 行")
	}

	// 不应包含 Actuator endpoints
	if strings.Contains(output.Content, "Exposing") && strings.Contains(output.Content, "endpoint") {
		t.Error("不应包含 Actuator endpoint 行")
	}

	// 不应包含 DefaultSecurityFilterChain
	if strings.Contains(output.Content, "DefaultSecurityFilterChain") {
		t.Error("不应包含 Security filter chain 行")
	}

	// 不应包含 Root WebApplicationContext
	if strings.Contains(output.Content, "Root WebApplicationContext") {
		t.Error("不应包含 Root WebApplicationContext 行")
	}

	// 不应包含 EntityManagerFactory
	if strings.Contains(output.Content, "EntityManagerFactory") {
		t.Error("不应包含 EntityManagerFactory 行")
	}

	// 不应包含 JtaPlatformInitiator
	if strings.Contains(output.Content, "JtaPlatformInitiator") {
		t.Error("不应包含 JtaPlatformInitiator 行")
	}

	// 不应包含 PersistenceUnitInfo
	if strings.Contains(output.Content, "PersistenceUnitInfo") {
		t.Error("不应包含 PersistenceUnitInfo 行")
	}

	// 不应包含 Starting Servlet engine / Initializing Spring embedded
	if strings.Contains(output.Content, "Starting Servlet engine") {
		t.Error("不应包含 Starting Servlet engine 行")
	}
	if strings.Contains(output.Content, "Initializing Spring embedded") {
		t.Error("不应包含 Initializing Spring embedded 行")
	}

	// 压缩率 > 50%
	origLen := len(fixture)
	filtLen := len(output.Content)
	ratio := float64(filtLen) / float64(origLen) * 100
	t.Logf("Spring Boot 启动压缩比: %.1f%% (%d -> %d bytes)", ratio, origLen, filtLen)
	if ratio > 50 {
		t.Errorf("压缩率不够: %.1f%% (应小于50%%)", ratio)
	}
}

func TestSpringBootStreamFilter_Interface(t *testing.T) {
	var sf filter.StreamFilter = &SpringBootFilter{}
	_ = sf.NewStreamInstance()
}

func TestSpringBootStreamFilter_Startup(t *testing.T) {
	f := &SpringBootFilter{}
	proc := f.NewStreamInstance()
	fixture := loadFixture(t, "springboot_startup.txt")
	lines := strings.Split(fixture, "\n")

	var emitted []string
	for _, line := range lines {
		action, output := proc.ProcessLine(line)
		if action == filter.StreamEmit {
			emitted = append(emitted, output)
		}
	}

	joined := strings.Join(emitted, "\n")
	// Port info preserved
	if !strings.Contains(joined, "8080") {
		t.Error("should preserve port")
	}
	// Started preserved
	if !strings.Contains(joined, "Started") {
		t.Error("should preserve Started")
	}
	// Banner stripped
	if strings.Contains(joined, "____") {
		t.Error("should strip banner")
	}
	// HikariPool stripped
	if strings.Contains(joined, "HikariPool") {
		t.Error("should strip HikariPool")
	}
	// Tomcat port preserved
	if !strings.Contains(joined, "Tomcat started on port") {
		t.Error("should preserve Tomcat port info")
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

// TestSpringBootStreamFilter_Flush Flush 当前始终返回 nil（Spring Boot 无跨行缓冲），
// 锁定该契约避免后续误加状态而不维护 Flush。
func TestSpringBootStreamFilter_Flush(t *testing.T) {
	f := &SpringBootFilter{}
	proc := f.NewStreamInstance()
	if got := proc.Flush(0); got != nil {
		t.Errorf("exit=0 Flush 应为 nil，得到 %v", got)
	}
	if got := proc.Flush(1); got != nil {
		t.Errorf("exit=1 Flush 应为 nil，得到 %v", got)
	}
}
