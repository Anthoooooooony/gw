package java

import (
	"strings"

	"github.com/gw-cli/gw/filter"
)

// 注意：SpringBootFilter 未注册到全局注册表。
// java -jar 启动的 Spring Boot 应用是长驻进程，gw 的批量过滤模型（等待进程退出后过滤）
// 不适合长驻进程。这些过滤规则保留供未来流式过滤模式复用。
//
// func init() {
// 	filter.Register(&SpringBootFilter{})
// }

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

		// 去除 ASCII banner（包含 ____ 或 :: Spring Boot ::）
		if strings.Contains(trimmed, "____") ||
			strings.Contains(trimmed, ":: Spring Boot ::") ||
			strings.Contains(trimmed, "\\___|") ||
			strings.Contains(trimmed, "/ ___'") ||
			strings.Contains(trimmed, "\\___ |") ||
			strings.Contains(trimmed, "___)| |") ||
			strings.Contains(trimmed, "|____|") ||
			strings.Contains(trimmed, "=========|") {
			continue
		}

		// 去除 banner 装饰行（以 / 或 ( 或 \ 或 ' 开头的 banner 部分）
		if isBannerDecorationLine(trimmed) {
			continue
		}

		// 去除 Hibernate HHH000 日志
		if strings.Contains(line, "HHH0") || strings.Contains(line, "HHH9") {
			continue
		}

		// 去除 Tomcat 引擎内部信息
		if strings.Contains(line, "o.apache.catalina.core.Standard") {
			continue
		}
		if strings.Contains(line, "o.a.c.c.C.[Tomcat]") {
			continue
		}

		// 去除 Spring Data 扫描详情
		if strings.Contains(line, "RepositoryConfigurationDelegate") {
			continue
		}

		// 去除 profile fallback 行
		if strings.Contains(line, "falling back to") && strings.Contains(line, "default profile") {
			continue
		}

		// 去除 HikariCP 连接池
		if strings.Contains(line, "HikariPool") || strings.Contains(line, "HikariDataSource") {
			continue
		}

		// 去除 Actuator endpoints
		if strings.Contains(line, "Exposing") && strings.Contains(line, "endpoint") {
			continue
		}

		// 去除 Security filter chain
		if strings.Contains(line, "DefaultSecurityFilterChain") || strings.Contains(line, "Will not secure any request") {
			continue
		}

		// 去除 JMX registration
		if strings.Contains(line, "Registering beans for JMX") || strings.Contains(line, "JMX registrations") {
			continue
		}

		// 去除 Liquibase/Flyway
		if strings.Contains(line, "liquibase") || strings.Contains(line, "flywaydb") || strings.Contains(line, "Applied migration") {
			continue
		}

		// 去除 WebApplicationContext initialization
		if strings.Contains(line, "Root WebApplicationContext") {
			continue
		}

		// 去除 Servlet engine startup
		if strings.Contains(line, "Starting Servlet engine") || strings.Contains(line, "Initializing Spring embedded") {
			continue
		}

		// 去除 JPA EntityManagerFactory
		if strings.Contains(line, "EntityManagerFactory") || strings.Contains(line, "PersistenceUnitInfo") {
			continue
		}

		// 去除 JTA platform
		if strings.Contains(line, "JtaPlatformInitiator") {
			continue
		}

		// 去除 Bean definition overriding
		if strings.Contains(line, "Overriding bean definition") {
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

func (f *SpringBootFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	// 启动失败需要完整栈追踪，直接透传
	return nil
}

// isBannerDecorationLine 判断是否为 Spring Boot ASCII banner 的装饰行
func isBannerDecorationLine(trimmed string) bool {
	if len(trimmed) == 0 {
		return false
	}
	// banner 行通常以这些字符开头
	bannerPrefixes := []string{"/\\\\", "( (", "\\\\/", "'  |"}
	for _, p := range bannerPrefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}
