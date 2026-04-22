# gw Makefile — 本地开发常用命令，与 CI 对齐。
# 真实 CI 定义在 .github/workflows/ci.yml，本文件只保留与之等价的命令，
# 方便开发者本地复现 CI 结果。

.PHONY: build test test-fast lint vet vuln bump-test tidy fmt ci clean

build: ## 本地构建 gw 二进制（CGO）
	CGO_ENABLED=1 go build -o gw .

test: ## 与 CI test job 对齐：race + cover + 原子 cover 模式
	CGO_ENABLED=1 go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...

test-fast: ## 快速跑（不带 race，不写 cover），便于反复迭代
	go test ./...

lint: ## golangci-lint run（需本地装 v2.2.2+）
	golangci-lint run --timeout=5m

vet: ## go vet
	go vet ./...

vuln: ## govulncheck（需 go install golang.org/x/vuln/cmd/govulncheck@v1.2.0）
	govulncheck ./...

bump-test: ## 跑 scripts/bump_test.sh（bash 单测）
	bash scripts/bump_test.sh

tidy: ## 与 CI "go mod tidy check" 等价，残留 diff 则失败
	go mod tidy
	git diff --exit-code go.mod go.sum

fmt: ## 按 gofmt 规范就地格式化
	gofmt -w .

ci: tidy vet test bump-test ## 跑 CI 核心 gate 的本地等价集

clean: ## 删除本地构建产物
	rm -f gw coverage.out
