# 03: 手动刷新现货，核对可用和锁定数量

**What to build:** 让用户对指定账号手动刷新现货，在明细查看采集时间和原币余额；建立后续钱包可复用的刷新入口。

**Blocked by:** 01, 02

**Status:** done

验收条件（使用合成数据）：

- [x] 合成响应含可用 0.08 BTC、锁定 0.02 BTC；保存后再查询，页面明细合计 0.10 BTC，不再额外加挂单占用。
- [x] 同账号同钱包的请求在途时再次刷新，返回 already_running 和同一批次 ID，只发起一次采集。
- [x] 仅刷新现货不能生成完整四钱包总额；无权限时显示原因，不保存成零余额。

## 复现与验收

项目根目录运行 `./scripts/snapshot-demo.sh verify`，在全新临时 PostgreSQL 中执行 Go 套件。本票核心行为见 `internal/current/*_test.go`；单包测试可运行 `go test -v ./internal/current`（数据库用例需通过前述临时库脚本运行）。[多账号合成工作区](../../../README.md#运行多账号工作区)用于从页面核对。
