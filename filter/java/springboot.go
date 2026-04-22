package java

import (
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&SpringBootFilter{})
}

// SpringBootFilter 过滤 Spring Boot 启动日志，压缩 banner 和内部引擎信息
type SpringBootFilter struct{}

func (f *SpringBootFilter) Name() string { return "java/springboot" }

func (f *SpringBootFilter) Match(cmd string, args []string) bool {
	if cmd != "java" {
		return false
	}
	for _, arg := range args {
		if strings.HasSuffix(arg, ".jar") {
			return true
		}
	}
	return false
}

func (f *SpringBootFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout + input.Stderr
	lines := strings.Split(original, "\n")

	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 去除 ASCII banner
		if isSpringBootBannerLine(trimmed) {
			continue
		}

		// 去除 banner 装饰行
		if isBannerDecorationLine(trimmed) {
			continue
		}

		if isSpringBootNoise(line) {
			continue
		}

		filtered = append(filtered, line)
	}

	content := collapseBlankLines(filtered)
	return filter.FilterOutput{
		Content:  content,
		Original: original,
	}
}

// isSpringBootBannerLine 判断是否为 Spring Boot ASCII banner 核心行
func isSpringBootBannerLine(trimmed string) bool {
	return strings.Contains(trimmed, "____") ||
		strings.Contains(trimmed, ":: Spring Boot ::") ||
		strings.Contains(trimmed, ":: Built with Spring Boot ::") ||
		strings.Contains(trimmed, "\\___|") ||
		strings.Contains(trimmed, "/ ___'") ||
		strings.Contains(trimmed, "\\___ |") ||
		strings.Contains(trimmed, "___)| |") ||
		strings.Contains(trimmed, "|____|") ||
		strings.Contains(trimmed, "=========|") ||
		strings.Contains(trimmed, "=====/") // Petclinic banner ending pattern
}

// isSpringBootNoise 判断是否为 Spring Boot 启动噪音行
func isSpringBootNoise(line string) bool {
	// Hibernate 日志（通过 logger name 匹配，而非内容匹配，避免误伤业务日志）
	if strings.Contains(line, "org.hibernate") || strings.Contains(line, "o.hibernate") || strings.Contains(line, "o.h.") {
		return true
	}
	// Tomcat 引擎内部信息
	if strings.Contains(line, "o.apache.catalina.core.Standard") {
		return true
	}
	if strings.Contains(line, "o.a.c.c.C.[Tomcat]") {
		return true
	}
	// Spring Data 扫描详情
	if strings.Contains(line, "RepositoryConfigurationDelegate") {
		return true
	}
	// profile fallback 行
	if strings.Contains(line, "falling back to") && strings.Contains(line, "default profile") {
		return true
	}
	// HikariCP 连接池
	if strings.Contains(line, "HikariPool") || strings.Contains(line, "HikariDataSource") {
		return true
	}
	// Actuator endpoints
	if strings.Contains(line, "Exposing") && strings.Contains(line, "endpoint") {
		return true
	}
	// Security filter chain
	if strings.Contains(line, "DefaultSecurityFilterChain") || strings.Contains(line, "Will not secure any request") {
		return true
	}
	// JMX registration
	if strings.Contains(line, "Registering beans for JMX") || strings.Contains(line, "JMX registrations") {
		return true
	}
	// Liquibase/Flyway
	if strings.Contains(line, "liquibase") || strings.Contains(line, "flywaydb") || strings.Contains(line, "Applied migration") {
		return true
	}
	// WebApplicationContext initialization
	if strings.Contains(line, "Root WebApplicationContext") {
		return true
	}
	// Servlet engine startup
	if strings.Contains(line, "Starting Servlet engine") || strings.Contains(line, "Initializing Spring embedded") {
		return true
	}
	// JPA EntityManagerFactory
	if strings.Contains(line, "EntityManagerFactory") || strings.Contains(line, "PersistenceUnitInfo") {
		return true
	}
	// JTA platform
	if strings.Contains(line, "JtaPlatformInitiator") {
		return true
	}
	// Bean definition overriding
	if strings.Contains(line, "Overriding bean definition") {
		return true
	}
	return false
}

func (f *SpringBootFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	// 启动失败需要完整栈追踪，直接透传
	return nil
}

// isBannerDecorationLine 判断是否为 Spring Boot ASCII banner 的装饰行
func isBannerDecorationLine(trimmed string) bool {
	if len(trimmed) == 0 {
		return false
	}
	// 标准 Spring Boot banner 行前缀
	bannerPrefixes := []string{"/\\\\", "( (", "\\\\/", "'  |"}
	for _, p := range bannerPrefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	// Petclinic 风格的 ASCII art banner (cat art)
	if strings.HasPrefix(trimmed, "|\\") || // cat ear
		strings.HasPrefix(trimmed, "/,`") || // cat head
		(strings.HasPrefix(trimmed, "|") && strings.Contains(trimmed, "|") && strings.Count(trimmed, "|") >= 4) { // table-like banner rows
		return true
	}
	return false
}

// NewStreamInstance 创建流式处理器实例
func (f *SpringBootFilter) NewStreamInstance() filter.StreamProcessor {
	return &springBootStreamProcessor{}
}

type springBootStreamProcessor struct {
	inBanner bool
}

func (p *springBootStreamProcessor) ProcessLine(line string) (filter.StreamAction, string) {
	trimmed := strings.TrimSpace(line)

	// Banner 检测
	if isSpringBootBannerLine(trimmed) || isBannerDecorationLine(trimmed) {
		p.inBanner = true
		return filter.StreamDrop, ""
	}
	if p.inBanner && trimmed == "" {
		p.inBanner = false
		return filter.StreamDrop, ""
	}

	if isSpringBootNoise(line) {
		return filter.StreamDrop, ""
	}

	return filter.StreamEmit, line
}

func (p *springBootStreamProcessor) Flush(exitCode int) []string {
	return nil
}
