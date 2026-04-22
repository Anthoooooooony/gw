package filter_test

// 场景化压缩率回归测试（baseline + tolerance 模式）。
//
// 思路借鉴 snapshot testing（Jest / Insta）和 Go golden file：
//   1. testdata/scenario_baseline.json 入库，记录每个场景的当前字节数和压缩率
//   2. 测试断言 |current_ratio - baseline_ratio| ≤ tolerance（默认 2 个百分点）
//   3. 规则有意改进后跑 `go test -run TestScenarioCompression -args -update`
//      重新生成 baseline；PR diff 里直接看到 "77% → 85%" 的压缩率跃迁
//
// 同时每次运行都会写 scenario-compression-report.md 方便 CI summary 贴出全表。

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Anthoooooooony/gw/filter"

	// 触发所有过滤器的 init() 自注册
	_ "github.com/Anthoooooooony/gw/filter/all"
)

// updateBaseline=true 时 TestScenarioCompression 写入新的 baseline 文件代替断言，
// 用法：go test -run TestScenarioCompression ./filter/ -args -update
var updateBaseline = flag.Bool("update", false, "重新生成 scenario_baseline.json")

// 压缩率容差（百分点）。2pp 对应 0.02 的 ratio 绝对差。
const tolerancePP = 2.0

// 短 fixture（例如 git status clean 2 行、springboot 启动头）在字节层面波动大，
// 用 absBytesTolerance 兜底：原始长度小于 256B 时放宽到任意变化皆可接受。
const smallFixtureBytes = 256

type scenarioMode int

const (
	modeBatch scenarioMode = iota
	modeStream
)

func (m scenarioMode) String() string {
	if m == modeStream {
		return "stream"
	}
	return "batch"
}

type scenario struct {
	name     string
	cmd      string
	args     []string
	fixture  string // 相对 filter/ 包目录
	exitCode int
	mode     scenarioMode
}

// scenarios 列举需要回归的命令/输出组合。增加新场景的流程：
//  1. 在 testdata/ 放 fixture 原始输出
//  2. 这里加一行
//  3. 跑 `go test ./filter/ -run TestScenarioCompression -args -update` 生成 baseline
//
// fixture 来源：
//   - mvn_*.txt / gradle_*.txt：spring-projects/spring-petclinic 真实构建输出
//     ·mvn_compile_failure.txt：人工注入 `private Undefined broken;` 触发 javac error
//     ·mvn_test_failure.txt：Postgres integration 因缺 Docker 而失败（真实 infra 缺失场景）
//     ·gradle_test_failure.txt：ValidatorTests 断言反向修改后的失败
//   - git_*.txt：gw 仓库自身真实输出
var scenarios = []scenario{
	// ---- Maven（spring-petclinic 真实场景）----
	{"mvn compile (success, batch)", "mvn", []string{"compile"}, "java/testdata/mvn_compile_success.txt", 0, modeBatch},
	{"mvn compile (failure, batch)", "mvn", []string{"compile"}, "java/testdata/mvn_compile_failure.txt", 1, modeBatch},
	{"mvn test (success, batch)", "mvn", []string{"test"}, "java/testdata/mvn_test_success.txt", 0, modeBatch},
	{"mvn test (failure, batch)", "mvn", []string{"test"}, "java/testdata/mvn_test_failure.txt", 1, modeBatch},
	{"mvn package (success, batch)", "mvn", []string{"package"}, "java/testdata/mvn_package_success.txt", 0, modeBatch},
	{"mvn test (success, stream)", "mvn", []string{"test"}, "java/testdata/mvn_test_success.txt", 0, modeStream},
	{"mvn test (failure, stream)", "mvn", []string{"test"}, "java/testdata/mvn_test_failure.txt", 1, modeStream},

	// ---- Gradle（spring-petclinic 真实场景）----
	{"gradle build (success, batch)", "gradle", []string{"build"}, "java/testdata/gradle_build_success.txt", 0, modeBatch},
	{"gradle test (success, batch)", "gradle", []string{"test"}, "java/testdata/gradle_test_success.txt", 0, modeBatch},
	{"gradle test (failure, batch)", "gradle", []string{"test"}, "java/testdata/gradle_test_failure.txt", 1, modeBatch},
	{"gradle build (success, stream)", "gradle", []string{"build"}, "java/testdata/gradle_build_success.txt", 0, modeStream},
	{"gradle test (success, stream)", "gradle", []string{"test"}, "java/testdata/gradle_test_success.txt", 0, modeStream},
	{"gradle test (failure, stream)", "gradle", []string{"test"}, "java/testdata/gradle_test_failure.txt", 1, modeStream},

	// ---- Git（gw 仓库自身）----
	{"git status (clean, batch)", "git", []string{"status"}, "git/testdata/git_status_clean.txt", 0, modeBatch},
	{"git status (dirty, batch)", "git", []string{"status"}, "git/testdata/git_status_dirty.txt", 0, modeBatch},
	{"git log (default, batch)", "git", []string{"log"}, "git/testdata/git_log_default.txt", 0, modeBatch},

	// ---- TOML 规则过滤器（真实项目 / 本机 state）----
	// Docker：本机 Docker Desktop 状态 + nginx:1.25-alpine pull / compose 管理 web+cache
	{"docker ps (batch)", "docker", []string{"ps"}, "toml/testdata/docker_ps.txt", 0, modeBatch},
	{"docker images (batch)", "docker", []string{"images"}, "toml/testdata/docker_images.txt", 0, modeBatch},
	{"docker pull (batch)", "docker", []string{"pull", "nginx:1.25-alpine"}, "toml/testdata/docker_pull.txt", 0, modeBatch},
	{"docker compose up (batch)", "docker", []string{"compose", "up", "-d"}, "toml/testdata/docker_compose_up.txt", 0, modeBatch},
	{"docker compose down (batch)", "docker", []string{"compose", "down"}, "toml/testdata/docker_compose_down.txt", 0, modeBatch},
	// npm：chalk/chalk
	{"npm install (batch)", "npm", []string{"install"}, "toml/testdata/npm_install.txt", 0, modeBatch},
	{"npm test (success, batch)", "npm", []string{"test"}, "toml/testdata/npm_test_success.txt", 0, modeBatch},
	{"npm test (failure, batch)", "npm", []string{"test"}, "toml/testdata/npm_test_failure.txt", 1, modeBatch},
	// pnpm：sindresorhus/is 实测捕获
	{"pnpm install (batch)", "pnpm", []string{"install"}, "toml/testdata/pnpm_install.txt", 0, modeBatch},
	{"pnpm test (failure, batch)", "pnpm", []string{"test"}, "toml/testdata/pnpm_test_failure.txt", 1, modeBatch},
	// vitest：本地 minimal fixture
	{"vitest (success, batch)", "vitest", []string{"run"}, "toml/testdata/vitest_success.txt", 0, modeBatch},
	{"vitest (failure, batch)", "vitest", []string{"run"}, "toml/testdata/vitest_failure.txt", 1, modeBatch},
	// jest：本地 minimal fixture（npm test 调用 jest）
	{"jest (success, batch)", "npm", []string{"test"}, "toml/testdata/jest_success.txt", 0, modeBatch},
	{"jest (failure, batch)", "npm", []string{"test"}, "toml/testdata/jest_failure.txt", 1, modeBatch},
	// Python：python-attrs/attrs
	{"pip install (batch)", "pip", []string{"install"}, "toml/testdata/pip_install.txt", 0, modeBatch},
	{"pytest (success, batch)", "pytest", nil, "toml/testdata/pytest_success.txt", 0, modeBatch},
	{"pytest (failure, batch)", "pytest", nil, "toml/testdata/pytest_failure.txt", 1, modeBatch},
	// Rust：BurntSushi/ripgrep
	{"cargo build (success, batch)", "cargo", []string{"build"}, "toml/testdata/cargo_build_success.txt", 0, modeBatch},
	{"cargo build (failure, batch)", "cargo", []string{"build"}, "toml/testdata/cargo_build_failure.txt", 1, modeBatch},
	{"cargo test (success, batch)", "cargo", []string{"test"}, "toml/testdata/cargo_test_success.txt", 0, modeBatch},
	{"cargo test (failure, batch)", "cargo", []string{"test"}, "toml/testdata/cargo_test_failure.txt", 1, modeBatch},
}

// baselineEntry 是 scenario_baseline.json 里的单条记录。
type baselineEntry struct {
	Name      string  `json:"name"`
	Mode      string  `json:"mode"`
	OrigBytes int     `json:"orig_bytes"`
	OutBytes  int     `json:"out_bytes"`
	Ratio     float64 `json:"ratio"` // 0.992 表示压缩 99.2%
}

const baselineFile = "testdata/scenario_baseline.json"

// scenarioRun 是单次运行的完整结果（含原文 + 压缩文本），供 writeReport 渲染
// 折叠式 before/after 对比；不入 baseline json（体积过大）。
type scenarioRun struct {
	entry      baselineEntry
	raw        string
	compressed string
}

func TestScenarioCompression(t *testing.T) {
	runs := make([]scenarioRun, 0, len(scenarios))
	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			raw, err := os.ReadFile(sc.fixture)
			if err != nil {
				t.Fatalf("读取 fixture 失败 %s: %v", sc.fixture, err)
			}
			out := runScenario(t, sc, string(raw))
			entry := baselineEntry{
				Name:      sc.name,
				Mode:      sc.mode.String(),
				OrigBytes: len(raw),
				OutBytes:  len(out),
				Ratio:     compressionRatio(len(raw), len(out)),
			}
			runs = append(runs, scenarioRun{entry: entry, raw: string(raw), compressed: out})

			if *updateBaseline {
				return // update 模式只收集，不断言
			}

			base, ok := loadBaselineEntry(t, sc.name)
			if !ok {
				t.Fatalf("baseline 缺少 %q。先跑 `go test ./filter/ -run TestScenarioCompression -args -update` 生成", sc.name)
			}
			// 短 fixture：放宽——只要不退化到原始长度的 120% 以上即可
			if base.OrigBytes < smallFixtureBytes {
				if entry.OutBytes > int(float64(base.OrigBytes)*1.2) {
					t.Errorf("%s: 短 fixture 输出异常膨胀 %d → %d", sc.name, base.OrigBytes, entry.OutBytes)
				}
				return
			}
			delta := (entry.Ratio - base.Ratio) * 100 // 转换成百分点
			if math.Abs(delta) > tolerancePP {
				t.Errorf("%s: 压缩率 %.2f%% 偏离 baseline %.2f%% 超过容差 %.1fpp (Δ=%+.2fpp)。若为有意改进，跑 -args -update 更新 baseline",
					sc.name, entry.Ratio*100, base.Ratio*100, tolerancePP, delta)
			}
		})
	}

	if *updateBaseline {
		entries := make([]baselineEntry, len(runs))
		for i, r := range runs {
			entries[i] = r.entry
		}
		if err := writeBaseline(entries); err != nil {
			t.Errorf("写 baseline 失败: %v", err)
		} else {
			t.Logf("baseline 已更新：%s", baselineFile)
		}
	}

	if err := writeReport(runs); err != nil {
		t.Errorf("写 scenario-compression-report.md 失败: %v", err)
	}
}

// runScenario 按场景 mode 调批量或流式过滤器，返回压缩后字符串。
func runScenario(t *testing.T, sc scenario, raw string) string {
	t.Helper()

	if sc.mode == modeStream {
		sf := filter.FindStream(sc.cmd, sc.args)
		if sf == nil {
			t.Fatalf("未找到匹配的 StreamFilter: %s %v", sc.cmd, sc.args)
		}
		proc := sf.NewStreamInstance()
		var buf strings.Builder
		for _, line := range strings.Split(raw, "\n") {
			action, out := proc.ProcessLine(line)
			if action == filter.StreamEmit {
				buf.WriteString(out)
				buf.WriteByte('\n')
			}
		}
		for _, extra := range proc.Flush(sc.exitCode) {
			buf.WriteString(extra)
			buf.WriteByte('\n')
		}
		return buf.String()
	}

	m := filter.GlobalRegistry().Find(sc.cmd, sc.args)
	if m == nil {
		t.Fatalf("未找到匹配的 Filter: %s %v", sc.cmd, sc.args)
	}
	in := filter.FilterInput{
		Cmd:      sc.cmd,
		Args:     sc.args,
		Stdout:   raw,
		ExitCode: sc.exitCode,
	}
	if sc.exitCode == 0 {
		return m.Filter.Apply(in).Content
	}
	out := m.Filter.ApplyOnError(in)
	if out == nil {
		return raw
	}
	return out.Content
}

func compressionRatio(orig, out int) float64 {
	if orig == 0 {
		return 0
	}
	return 1 - float64(out)/float64(orig)
}

// ---- baseline 读写 ----

// baselineCache 避免每个 subtest 重复 IO。首次 load 失败会保留为空 map。
var baselineCache map[string]baselineEntry

func loadBaselineEntry(t *testing.T, name string) (baselineEntry, bool) {
	t.Helper()
	if baselineCache == nil {
		data, err := os.ReadFile(baselineFile)
		if err != nil {
			t.Fatalf("读取 baseline 失败 %s: %v", baselineFile, err)
		}
		var entries []baselineEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			t.Fatalf("解析 baseline 失败: %v", err)
		}
		baselineCache = make(map[string]baselineEntry, len(entries))
		for _, e := range entries {
			baselineCache[e.Name] = e
		}
	}
	e, ok := baselineCache[name]
	return e, ok
}

func writeBaseline(entries []baselineEntry) error {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(baselineFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(baselineFile, data, 0o644)
}

// ---- 压缩率 markdown 报告（给 CI $GITHUB_STEP_SUMMARY 贴）----

// 单个 <details> 里原文/压缩块的截断上限。GitHub step summary 限 1 MiB，
// 29 个 scenario 各 ~8 KB × 2 块预留到 ~450 KB，留足余量给表头等。
const reportSnippetMaxBytes = 8 * 1024

func writeReport(runs []scenarioRun) error {
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].entry.Name < runs[j].entry.Name })

	baseMap := map[string]baselineEntry{}
	if data, err := os.ReadFile(baselineFile); err == nil {
		var entries []baselineEntry
		if json.Unmarshal(data, &entries) == nil {
			for _, e := range entries {
				baseMap[e.Name] = e
			}
		}
	}

	var buf strings.Builder
	buf.WriteString("# gw 场景化压缩率报告\n\n")
	buf.WriteString("由 `go test ./filter/ -run TestScenarioCompression` 自动生成。")
	buf.WriteString("Δ 列为当前压缩率 − baseline（百分点），|Δ| > ")
	fmt.Fprintf(&buf, "%.1fpp 视为回归。\n\n", tolerancePP)
	buf.WriteString("| 场景 | 模式 | 原始 | 压缩后 | 压缩率 | baseline | Δ (pp) |\n")
	buf.WriteString("|------|------|------|--------|--------|----------|--------|\n")
	for _, r := range runs {
		e := r.entry
		baseCell := "—"
		deltaCell := "—"
		if b, ok := baseMap[e.Name]; ok {
			baseCell = fmt.Sprintf("%.1f%%", b.Ratio*100)
			deltaCell = fmt.Sprintf("%+.2f", (r.entry.Ratio-b.Ratio)*100)
		}
		fmt.Fprintf(&buf, "| %s | %s | %s | %s | %.1f%% | %s | %s |\n",
			e.Name, e.Mode, humanBytes(e.OrigBytes), humanBytes(e.OutBytes),
			e.Ratio*100, baseCell, deltaCell)
	}

	// 每 scenario 的原文/压缩后对比，折叠显示。
	buf.WriteString("\n## 场景原文对比（点击展开）\n\n")
	buf.WriteString(fmt.Sprintf("每块内容超过 %d B 时截断首尾保留；完整内容见 artifact 或 fixture 源文件。\n\n", reportSnippetMaxBytes))
	for _, r := range runs {
		renderScenarioDetail(&buf, r)
	}

	return os.WriteFile("scenario-compression-report.md", []byte(buf.String()), 0o644)
}

// renderScenarioDetail 输出一个场景的折叠块，含原文 + 压缩后两段嵌套 <details>。
// 使用 HTML <details>/<summary>，这是 GFM + GitHub step summary 都支持的折叠语法。
func renderScenarioDetail(buf *strings.Builder, r scenarioRun) {
	e := r.entry
	fmt.Fprintf(buf, "<details>\n<summary><b>%s</b> — %s, %.1f%% (%s → %s)</summary>\n\n",
		htmlEscape(e.Name), e.Mode, e.Ratio*100, humanBytes(e.OrigBytes), humanBytes(e.OutBytes))

	fmt.Fprintf(buf, "<details>\n<summary>原始 (%s)</summary>\n\n~~~text\n%s\n~~~\n\n</details>\n\n",
		humanBytes(e.OrigBytes), truncateSnippet(r.raw))

	fmt.Fprintf(buf, "<details>\n<summary>压缩后 (%s)</summary>\n\n~~~text\n%s\n~~~\n\n</details>\n\n",
		humanBytes(e.OutBytes), truncateSnippet(r.compressed))

	buf.WriteString("</details>\n\n")
}

// truncateSnippet 对超长内容截取头尾，避免一个 fixture 吃掉整个 summary 配额。
// 用 "~~~" fence 是为了避免与构建输出里常见的 ``` 冲突（大部分 mvn/gradle/pytest
// 不会打印 "~~~"）。
func truncateSnippet(s string) string {
	if len(s) <= reportSnippetMaxBytes {
		return s
	}
	half := reportSnippetMaxBytes / 2
	return s[:half] + fmt.Sprintf("\n\n... (中间 %s 被截断) ...\n\n", humanBytes(len(s)-reportSnippetMaxBytes)) + s[len(s)-half:]
}

// htmlEscape 仅处理 <summary> 里可能出现的字符，内容在 ~~~ 块内不需要转义。
func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func humanBytes(n int) string {
	if n >= 1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%d B", n)
}
