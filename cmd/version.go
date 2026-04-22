package cmd

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// Version 由 ldflags 注入，例如：
//
//	go build -ldflags "-X github.com/Anthoooooooony/gw/cmd.Version=v0.1.0" .
//
// 未注入时保持 "dev"，fallback 到 runtime/debug.ReadBuildInfo()。
var Version = "dev"

// Commit 可选：ldflags 注入的 git commit，优先级高于 build info。
var Commit = ""

// BuildDate 可选：ldflags 注入的构建日期，优先级高于 build info。
var BuildDate = ""

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "打印 gw 版本信息",
	Long:  "打印版本、git commit、构建日期和 Go 版本。",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(versionString())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	// 同时暴露为 `gw --version`（cobra 内置）
	rootCmd.Version = versionString()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
}

// versionString 拼装最终的版本输出字符串。
// 版本号优先级：ldflags Version（非 "dev"）> build info vcs.revision > "dev (unknown build)"。
func versionString() string {
	ver := Version
	commit := Commit
	date := BuildDate

	// 未注入时尝试从 runtime/debug 读取 VCS 元数据
	if commit == "" || date == "" || ver == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, s := range info.Settings {
				switch s.Key {
				case "vcs.revision":
					if commit == "" {
						commit = s.Value
					}
				case "vcs.time":
					if date == "" {
						date = s.Value
					}
				}
			}
			// ldflags 没给 Version 时，尝试用 module version
			if ver == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
				ver = info.Main.Version
			}
		}
	}

	// 什么都没有的极端情况
	if ver == "dev" && commit == "" && date == "" {
		return fmt.Sprintf("gw version dev (unknown build, %s)", runtime.Version())
	}

	shortCommit := commit
	if len(shortCommit) > 7 {
		shortCommit = shortCommit[:7]
	}

	parts := []string{fmt.Sprintf("gw version %s", ver)}
	meta := []string{}
	if shortCommit != "" {
		meta = append(meta, "commit "+shortCommit)
	}
	if date != "" {
		meta = append(meta, "built "+date)
	}
	meta = append(meta, runtime.Version())
	parts = append(parts, "("+strings.Join(meta, ", ")+")")
	return strings.Join(parts, " ")
}
