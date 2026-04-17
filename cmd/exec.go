package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gw-cli/gw/filter"
	"github.com/gw-cli/gw/internal"
	"github.com/gw-cli/gw/track"
	"github.com/spf13/cobra"
)

// extractDumpRawFlag 在 DisableFlagParsing 的环境下手动解析并剥离
// --dump-raw=PATH 或 --dump-raw PATH 参数。
// 返回：剩余 args、dump 目标路径、是否找到该 flag。
//
// 扫描策略：
//   - 只扫描命令名之前的参数（即首个不以 `--` 开头的 token 之前）
//   - 遇到 --dump-raw=VAL 或 --dump-raw 后跟一个 token 即提取
//   - 未被识别为 gw exec flag 的 -- 开头参数保留在 rest 中，原样透传
//
// 这样既支持 `gw exec --dump-raw=/tmp/a git status`，也支持
// `gw exec --dump-raw /tmp/a git status`，同时不会误吞子命令的同名 flag
// （例如 `gw exec my-tool --dump-raw=/inside`，此处命令名之后的 --dump-raw
// 属于 my-tool 自己的参数）。
func extractDumpRawFlag(args []string) (rest []string, path string, found bool) {
	if len(args) == 0 {
		return args, "", false
	}
	out := make([]string, 0, len(args))
	i := 0
	for i < len(args) {
		a := args[i]
		// 首个不以 -- 开头的 token 视为命令名，扫描停止
		if !strings.HasPrefix(a, "--") {
			break
		}
		if !found {
			switch {
			case a == "--dump-raw":
				if i+1 >= len(args) {
					// 缺失值：视为未识别，归还原样
					out = append(out, a)
					i++
					continue
				}
				path = args[i+1]
				found = true
				i += 2
				continue
			case strings.HasPrefix(a, "--dump-raw="):
				path = strings.TrimPrefix(a, "--dump-raw=")
				found = true
				i++
				continue
			}
		}
		// 其它 -- 开头的 flag 透传到 rest（可能是未来 gw exec 新增的 flag）
		out = append(out, a)
		i++
	}
	// 追加剩余（命令名及之后全部原样）
	out = append(out, args[i:]...)
	return out, path, found
}

var execCmd = &cobra.Command{
	Use:                "exec [command] [args...]",
	Short:              "执行命令并过滤输出",
	Long:               "执行指定命令，通过匹配的过滤器压缩输出以减少 token 消耗。",
	DisableFlagParsing: true,
	Run:                runExec,
}

func init() {
	rootCmd.AddCommand(execCmd)
}

func runExec(cmd *cobra.Command, args []string) {
	start := time.Now()

	// 0. 解析诊断逃生舱 --dump-raw，必须在 parse 命令名之前
	args, dumpRawPath, _ := extractDumpRawFlag(args)

	// 1. PARSE: 提取命令名和参数
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "gw exec: 缺少命令参数")
		os.Exit(1)
	}
	cmdName := args[0]
	cmdArgs := args[1:]

	// 2. ROUTE: 从注册表查找匹配的过滤器
	// 优先检查流式过滤器
	if sf := filter.FindStream(cmdName, cmdArgs); sf != nil {
		runStreamExec(sf, cmdName, cmdArgs, dumpRawPath)
		return
	}

	matched := filter.GlobalRegistry().Find(cmdName, cmdArgs)

	// 3. EXECUTE: 本地执行命令
	result, err := internal.RunCommand(cmdName, cmdArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gw exec: 无法执行命令: %v\n", err)
		os.Exit(127)
	}

	// 4. FILTER: 应用过滤器
	var output string
	var filterUsed string
	originalOutput := result.Stdout + result.Stderr

	// 诊断逃生舱：如指定了 --dump-raw，把原始输出写入文件（失败仅告警）
	if dumpRawPath != "" {
		if err := os.WriteFile(dumpRawPath, []byte(originalOutput), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "gw: warning: 写入 --dump-raw 文件 %s 失败: %v\n", dumpRawPath, err)
		}
	}

	if matched != nil {
		filterUsed = matched.Name()
		input := filter.FilterInput{
			Cmd:      cmdName,
			Args:     cmdArgs,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
			ExitCode: result.ExitCode,
		}

		if result.ExitCode == 0 {
			fo := matched.Apply(input)
			output = fo.Content
		} else {
			fo := matched.ApplyOnError(input)
			if fo != nil {
				output = fo.Content
			} else {
				output = originalOutput
			}
		}
	} else {
		// 无匹配过滤器，透传原始输出
		output = originalOutput
	}

	// 5. PRINT: 输出结果
	fmt.Print(output)

	// 6. TRACK: 记录到 SQLite（异步，不阻塞输出）
	inputTokens := track.EstimateTokens(originalOutput)
	outputTokens := track.EstimateTokens(output)
	savedTokens := inputTokens - outputTokens
	elapsedMs := time.Since(start).Milliseconds()
	fullCmd := cmdName
	if len(cmdArgs) > 0 {
		fullCmd = cmdName + " " + strings.Join(cmdArgs, " ")
	}

	if Verbose {
		fmt.Fprintf(os.Stderr, "gw: info: input_tokens=%d output_tokens=%d saved=%d elapsed=%dms\n",
			inputTokens, outputTokens, savedTokens, elapsedMs)
	}

	// 写入数据库（同步，在 os.Exit 前完成）
	// DB 打开失败属于非致命降级：verbose 时 warn，生产模式保持静默（主流程不受影响）。
	if db, err := track.NewDB(track.DefaultDBPath()); err == nil {
		rec := track.Record{
			Timestamp:    time.Now().UTC(),
			Command:      fullCmd,
			ExitCode:     result.ExitCode,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			SavedTokens:  savedTokens,
			ElapsedMs:    elapsedMs,
			FilterUsed:   filterUsed,
		}
		// 默认不落盘 raw_output（否则 DB 会爆炸）；仅 GW_STORE_RAW=1 时写入。
		if os.Getenv("GW_STORE_RAW") == "1" {
			rec.RawOutput = originalOutput
		}
		_ = db.InsertRecord(rec)
		db.Close()
	} else if Verbose {
		fmt.Fprintf(os.Stderr, "gw: warning: tracking DB open failed: %v\n", err)
	}

	// 7. 使用原始命令的退出码退出
	os.Exit(result.ExitCode)
}

func runStreamExec(sf filter.StreamFilter, cmdName string, cmdArgs []string, dumpRawPath string) {
	start := time.Now()
	proc := sf.NewStreamInstance()
	var originalChars int
	var filteredChars int

	// 诊断逃生舱：流式模式下边流式边累积写入 buffer，结束后一次性落盘。
	// 选择"先 buffer 后落盘"而非边流边 append 是因为：
	//   1) 避免每行 syscall，性能更好
	//   2) 写失败时不会产生半截文件
	//   3) 文件一旦打开不中断主流程
	var rawBuf strings.Builder
	storeRaw := os.Getenv("GW_STORE_RAW") == "1"

	var stderrBuf strings.Builder
	exitCode, err := internal.RunCommandStreamingFull(cmdName, cmdArgs, func(line string) {
		originalChars += len([]rune(line))
		if dumpRawPath != "" || storeRaw {
			rawBuf.WriteString(line)
			rawBuf.WriteByte('\n')
		}
		action, output := proc.ProcessLine(line)
		if action == filter.StreamEmit {
			filteredChars += len([]rune(output))
			fmt.Println(output)
		}
	}, &stderrBuf)

	if err != nil {
		fmt.Fprintf(os.Stderr, "gw exec: 无法执行命令: %v\n", err)
		os.Exit(127)
	}

	// stderr 透传 + 累积入 raw buffer（stderr 也是原始输出的一部分）
	if stderrBuf.Len() > 0 {
		if dumpRawPath != "" || storeRaw {
			rawBuf.WriteString(stderrBuf.String())
		}
		fmt.Fprint(os.Stderr, stderrBuf.String())
	}

	// 进程退出后尝试落盘 --dump-raw，失败只 warning
	if dumpRawPath != "" {
		if err := os.WriteFile(dumpRawPath, []byte(rawBuf.String()), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "gw: warning: 写入 --dump-raw 文件 %s 失败: %v\n", dumpRawPath, err)
		}
	}

	// Flush
	flushedLines := proc.Flush(exitCode)
	for _, line := range flushedLines {
		filteredChars += len([]rune(line))
		fmt.Println(line)
	}

	// TRACK
	elapsed := time.Since(start)
	inputTokens := track.EstimateTokensByLen(originalChars)
	outputTokens := track.EstimateTokensByLen(filteredChars)
	fullCmd := cmdName
	if len(cmdArgs) > 0 {
		fullCmd = cmdName + " " + strings.Join(cmdArgs, " ")
	}

	if Verbose {
		fmt.Fprintf(os.Stderr, "gw: info: %d → %d tokens (saved %d, elapsed %dms)\n",
			inputTokens, outputTokens, inputTokens-outputTokens, elapsed.Milliseconds())
	}

	// 写入数据库（同步，在 os.Exit 前完成）
	// DB 打开失败属于非致命降级：verbose 时 warn，生产模式保持静默。
	if db, err := track.NewDB(track.DefaultDBPath()); err == nil {
		rec := track.Record{
			Timestamp:    time.Now().UTC(),
			Command:      fullCmd,
			ExitCode:     exitCode,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			SavedTokens:  inputTokens - outputTokens,
			ElapsedMs:    elapsed.Milliseconds(),
			FilterUsed:   sf.Name() + ":stream",
		}
		if storeRaw {
			rec.RawOutput = rawBuf.String()
		}
		_ = db.InsertRecord(rec)
		db.Close()
	} else if Verbose {
		fmt.Fprintf(os.Stderr, "gw: warning: tracking DB open failed: %v\n", err)
	}

	// 退出码语义：
	//   - 正常退出 / 超时（124）/ 信号终止（128+signal）由 RunCommandStreamingFull 直接返回
	//   - 兜底：-1 表示未知错误（无法识别的 wait 失败），用 SIGINT 惯例的 130 稳妥收场
	if exitCode < 0 {
		exitCode = 130
	}
	os.Exit(exitCode)
}
