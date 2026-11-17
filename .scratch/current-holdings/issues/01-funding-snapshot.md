# 01: 查看一批资金持仓，缺价时保留股数

**What to build:** 用一个合成账号贯通 Go 十进制计算、PostgreSQL 存储、API 和「最新采集」页面，让用户切换钱包与资产类别核对同一批持仓。

**Blocked by:** 无

**Status:** done

验收条件（使用合成数据）：

- [x] 其他三个钱包同批确认为零，资金钱包现金价值 800 USD、2 股直接美股每股 100 USD：钱包视图资金为 1,000，类别视图美股为 200、其他为 800，两种视图的总额均为 1,000 USD。
- [x] 移除股票报价后，资金钱包行显示「钱包总额：—；已估值小计：800 USD；2 股缺价」，明细保留原始代码与 2 股。该行 valueUsd 为 null、pricedSubtotalUsd 为 "800"（或等值十进制字符串）；其余三个已确认空仓的钱包仍各显示 0。
- [x] 同一缺价批次切换到资产类别视图：「其他资产」完整金额为 800 USD，「美股」金额未知。两种视图的组合总额均为 null、已估值小计均为 800 USD；每一行的 share 都为 null，页面不显示占比。
- [x] 重新读取已保存的批次，API 以字符串传递十进制金额；最新观察不替代首页主卡与环图的日终数据。

## 复现与验收

从项目根目录按 [README 的演示步骤](../../../README.md#运行最新持仓演示) 安装前端依赖并构建。每次运行以下命令都创建独立临时 PostgreSQL，无需个人账号或密钥：

- `./scripts/snapshot-demo.sh complete`：登录本机页面，切换两种分组，核对 1,000、200、800。
- `./scripts/snapshot-demo.sh unpriced`：结束上一次演示后重新运行，核对资金总额「—」、小计 800、明细 EQ_MU 数量 2。空仓钱包金额为 0，两视图均没有有效占比。
- `./scripts/snapshot-demo.sh verify`：以合成完整批次和缺价批次运行 Go 与数据库/API 验收；写入新批后，按旧批次 ID 查询仍得到原来的估值。

前端：`npm --prefix web run lint`、`npm --prefix web run format:check`、`npm --prefix web run build`。两场景已在独立 Chrome 会话验证。
