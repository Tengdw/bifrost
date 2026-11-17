# 05: 刷新赚币，核对活期和定期本金

**What to build:** 刷新 Simple Earn，分别保存并展示活期产品和定期持仓，核对归入权益的本金。

**Blocked by:** 03

**Status:** done

验收条件（使用合成数据）：

- [x] 两路合成响应均需翻完所有页；活期本金 300 USDT、定期本金 200 USDT、累计奖励字段为 7 时，计入 500 USDT。
- [x] 页面可分别核对活期产品与定期仓位；赎回在途或抵押导致跨钱包归属不明时，展示部分数据及原因，不重复计入。
- [x] 任一必需分页失败，不把残缺结果标为该钱包的完整观察。

## 复现与验收

项目根目录运行 `./scripts/snapshot-demo.sh verify`，在全新临时 PostgreSQL 中执行 Go 套件。本票核心行为见 `internal/binance/*_test.go`；单包测试可运行 `go test -v ./internal/binance`（数据库用例需通过前述临时库脚本运行）。[多账号合成工作区](../../../README.md#运行多账号工作区)用于从页面核对。
