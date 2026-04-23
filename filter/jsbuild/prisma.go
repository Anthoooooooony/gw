package jsbuild

import (
	"strings"

	"github.com/Anthoooooooony/gw/filter"
)

func init() {
	filter.Register(&PrismaFilter{})
}

// PrismaFilter 压缩 prisma generate/migrate/db-push：
// 丢 Env/Schema 头、pris.ly Tip 链接、ASCII 迁移文件树。
type PrismaFilter struct{}

func (f *PrismaFilter) Name() string { return "jsbuild/prisma" }

func (f *PrismaFilter) Match(cmd string, args []string) bool {
	return cmd == "prisma"
}

// prismaNoisePrefixes 识别环境头行和营销 Tip 行。
var prismaNoisePrefixes = []string{
	"Environment variables loaded",
	"Prisma schema loaded",
	"Datasource \"",
	"Tip: ",
	"Start by importing",
}

func (f *PrismaFilter) Apply(input filter.FilterInput) filter.FilterOutput {
	original := input.Stdout
	content := filter.StripANSI(original)
	lines := strings.Split(content, "\n")

	var out []string
	for _, line := range lines {
		if isPrismaNoise(line) {
			continue
		}
		out = append(out, line)
	}

	joined := strings.Join(out, "\n")
	return filter.FilterOutput{
		Content:  collapseBlankLines(joined),
		Original: original,
	}
}

func (f *PrismaFilter) ApplyOnError(input filter.FilterInput) *filter.FilterOutput {
	return nil
}

func isPrismaNoise(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	for _, p := range prismaNoisePrefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}
