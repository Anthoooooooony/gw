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
		runStreamExec(sf, cmdName, cmdArgs)
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
		fmt.Fprintf(os.Stderr, "[gw] input_tokens=%d output_tokens=%d saved=%d elapsed=%dms\n",
			inputTokens, outputTokens, savedTokens, elapsedMs)
	}

	// 写入数据库（同步，在 os.Exit 前完成）
	if db, err := track.NewDB(track.DefaultDBPath()); err == nil {
		_ = db.InsertRecord(track.Record{
			Timestamp:    time.Now().UTC(),
			Command:      fullCmd,
			ExitCode:     result.ExitCode,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			SavedTokens:  savedTokens,
			ElapsedMs:    elapsedMs,
			FilterUsed:   filterUsed,
		})
		db.Close()
	}

	// 7. 使用原始命令的退出码退出
	os.Exit(result.ExitCode)
}

func runStreamExec(sf filter.StreamFilter, cmdName string, cmdArgs []string) {
	start := time.Now()
	proc := sf.NewStreamInstance()
	var originalChars int
	var filteredChars int

	var stderrBuf strings.Builder
	exitCode, err := internal.RunCommandStreamingFull(cmdName, cmdArgs, func(line string) {
		originalChars += len([]rune(line))
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

	// stderr 透传
	if stderrBuf.Len() > 0 {
		fmt.Fprint(os.Stderr, stderrBuf.String())
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
		fmt.Fprintf(os.Stderr, "[gw:stream] %d → %d tokens (saved %d, elapsed %dms)\n",
			inputTokens, outputTokens, inputTokens-outputTokens, elapsed.Milliseconds())
	}

	// 写入数据库（同步，在 os.Exit 前完成）
	if db, err := track.NewDB(track.DefaultDBPath()); err == nil {
		_ = db.InsertRecord(track.Record{
			Timestamp:    time.Now().UTC(),
			Command:      fullCmd,
			ExitCode:     exitCode,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			SavedTokens:  inputTokens - outputTokens,
			ElapsedMs:    elapsed.Milliseconds(),
			FilterUsed:   sf.Name() + ":stream",
		})
		db.Close()
	}

	// 负数退出码（信号终止）映射为 128+signal 的惯例值
	if exitCode < 0 {
		exitCode = 130 // SIGINT (Ctrl+C) 的标准退出码
	}
	os.Exit(exitCode)
}
