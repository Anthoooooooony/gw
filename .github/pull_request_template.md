<!--
PR 模板：用于降低 review 成本。勾选或补齐下面三节即可，无关节可整节删除。
-->

## Summary
<!-- 3 句内概括改动 -->

## Why
<!-- 动机 / 关联 issue，勿忘 `Closes #NN` / `Fixes #NN` 自动关联 -->

## Test plan
<!-- 勾选或补充验证方式 -->
- [ ] `go test -race ./...` 通过
- [ ] 触碰 `scripts/` 时跑 `bash scripts/bump_test.sh`
- [ ] 触碰 UI/CLI 交互时附上 `gw <subcmd>` 实际输出
- [ ] CI 8 层 gate 全绿（test×2 / build×2 / shellcheck / actionlint / golangci-lint / govulncheck）
