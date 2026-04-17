package toml

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Source 表示规则来源的前缀
const (
	SourceBuiltin = "builtin"
	sourceUser    = "user://"
	sourceProject = "project://"
)

// 用于测试时重定向路径的钩子
var (
	// userRulesDirFn 返回用户级规则目录（空字符串表示未配置）
	userRulesDirFn = defaultUserRulesDir
	// projectRulesRootFn 返回用于向上查找 .gw/rules 的起点目录
	projectRulesRootFn = defaultProjectRulesRoot
)

// LoadedRule 是 Loader 返回的一条规则及其元信息
type LoadedRule struct {
	ID     string // section.name，例如 docker.ps
	Rule   Rule
	Source string // builtin | user://<path> | project://<path>
}

// rawRuleFile 是从 TOML 文件解码的中间表示。
// 顶层是 section（如 docker），值是子名（如 ps）到 rawRule 的映射。
type rawRule struct {
	Match      string   `toml:"match"`
	StripAnsi  bool     `toml:"strip_ansi"`
	MaxLines   int      `toml:"max_lines"`
	HeadLines  int      `toml:"head_lines"`
	TailLines  int      `toml:"tail_lines"`
	StripLines []string `toml:"strip_lines"`
	KeepLines  []string `toml:"keep_lines"`
	OnEmpty    string   `toml:"on_empty"`
	Disabled   bool     `toml:"disabled"`
}

// LoadAllRules 按三级加载顺序（builtin → user → project）收集全部 TOML 规则，
// 高层同 ID 覆盖低层。解析错误只打 warning，不中断整个加载流程。
// 返回按 ID 排序的规则列表。
func LoadAllRules() []LoadedRule {
	// byID 用于同 ID 覆盖；disabled[ID]=true 表示被更高层剔除
	byID := make(map[string]LoadedRule)
	disabled := make(map[string]bool)

	// 1. builtin：go:embed
	loadEmbeddedInto(byID, disabled)

	// 2. user：$XDG_CONFIG_HOME/gw/rules/
	if dir := userRulesDirFn(); dir != "" {
		loadDirInto(dir, sourceUser, byID, disabled)
	}

	// 3. project：向上查找 .gw/rules/
	if root := projectRulesRootFn(); root != "" {
		if dir := findProjectRulesDir(root); dir != "" {
			loadDirInto(dir, sourceProject, byID, disabled)
		}
	}

	// 去除被禁用的规则
	out := make([]LoadedRule, 0, len(byID))
	for id, r := range byID {
		if disabled[id] {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// loadEmbeddedInto 从 go:embed 的 builtinRules 读取规则并注入 byID。
func loadEmbeddedInto(byID map[string]LoadedRule, disabled map[string]bool) {
	entries, err := builtinRules.ReadDir("rules")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gw: 读取内置规则目录失败: %v\n", err)
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		path := "rules/" + entry.Name()
		data, err := builtinRules.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gw: 读取内置规则 %s 失败: %v\n", path, err)
			continue
		}
		parseAndMerge(string(data), SourceBuiltin, byID, disabled)
	}
}

// loadDirInto 从指定目录扫描 *.toml 并按 sourcePrefix 注入 byID。
// sourcePrefix 形如 "user://" / "project://"，最终 source 字段为 prefix + 绝对路径。
func loadDirInto(dir, sourcePrefix string, byID map[string]LoadedRule, disabled map[string]bool) {
	info, err := os.Stat(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "gw: 访问规则目录 %s 失败: %v\n", dir, err)
		}
		return
	}
	if !info.IsDir() {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gw: 读取规则目录 %s 失败: %v\n", dir, err)
		return
	}
	// 按文件名排序以保证可重复性（同目录下同 ID 由后出现者覆盖）
	files := make([]fs.DirEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		files = append(files, e)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

	for _, e := range files {
		full := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gw: 读取规则文件 %s 失败: %v\n", full, err)
			continue
		}
		parseAndMerge(string(data), sourcePrefix+full, byID, disabled)
	}
}

// parseAndMerge 解析 TOML 文本，按 section.name 作为 ID 注入 byID，
// 同 ID 会被覆盖；disabled=true 的条目标记为剔除。
// 解析错误只打 warning。
func parseAndMerge(data, source string, byID map[string]LoadedRule, disabled map[string]bool) {
	var raw map[string]map[string]rawRule
	if _, err := toml.Decode(data, &raw); err != nil {
		fmt.Fprintf(os.Stderr, "gw: TOML 解析失败 (%s): %v\n", source, err)
		return
	}
	for section, group := range raw {
		for name, rr := range group {
			id := section + "." + name
			if rr.Disabled {
				disabled[id] = true
				// 同时剔除已存在的条目
				delete(byID, id)
				continue
			}
			// 明确取消 disabled 标记（若低层禁用、高层启用）
			delete(disabled, id)
			byID[id] = LoadedRule{
				ID: id,
				Rule: Rule{
					Match:      rr.Match,
					StripAnsi:  rr.StripAnsi,
					MaxLines:   rr.MaxLines,
					HeadLines:  rr.HeadLines,
					TailLines:  rr.TailLines,
					StripLines: rr.StripLines,
					KeepLines:  rr.KeepLines,
					OnEmpty:    rr.OnEmpty,
				},
				Source: source,
			}
		}
	}
}

// defaultUserRulesDir 返回 $XDG_CONFIG_HOME/gw/rules 或平台默认配置目录。
// os.UserConfigDir() 在 Linux 下遵循 XDG_CONFIG_HOME，Windows 使用 %AppData%，macOS 为 ~/Library/Application Support。
func defaultUserRulesDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return ""
	}
	return filepath.Join(base, "gw", "rules")
}

// defaultProjectRulesRoot 返回项目规则查找的起点：当前工作目录。
func defaultProjectRulesRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

// findProjectRulesDir 从 start 向上查找 .gw/rules 目录。
// 停止条件：
//  1. 找到 .gw/rules 目录，返回它；
//  2. 当前目录是文件系统根（再往上等于自身）；
//  3. 当前目录含有 .git（典型项目边界）——此时仍会先检查 .gw/rules。
func findProjectRulesDir(start string) string {
	cur := start
	for {
		candidate := filepath.Join(cur, ".gw", "rules")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		// 命中项目边界 .git 后停止（不再继续向上）
		if info, err := os.Stat(filepath.Join(cur, ".git")); err == nil && info.IsDir() {
			return ""
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}
