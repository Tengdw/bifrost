# Bifrost

Bifrost 是个人使用的币安多账号资产看板，用来查看各钱包的美元估值、历史持仓和每日盈亏。首版覆盖现货、资金、Simple Earn 活期与定期、普通 U 本位合约，以及资金钱包内直接持有的币安美股。

**当前状态：** 第 01 票提供本机合成持仓演示（Go、PostgreSQL、React），支持「最新采集」总览与同批明细。独立 UI 原型仍可离线查看；尚未接入真实账户或部署生产服务。

## 从哪里开始

| 目的 | 入口 |
|---|---|
| 了解首版范围、验收标准与实施顺序 | [系统总规格](./spec/SPEC.md) |
| 查阅 API、数据模型、估值口径和运维约定 | [专项文档索引](./Docs/README.md) |
| 体验账号切换、收益曲线及盈亏日历 | [离线看板](./ui-preview/dist/index.html) · [界面预览](./ui-preview/preview.png) |

## 运行最新持仓演示

需要 Go 1.25.7、Node.js 22.13+、PostgreSQL 17 或 18 的本机服务端工具（工作目录无需运行数据库）。若没有 pg_config，将 PGBIN 指向 initdb 和 pg_ctl 所在目录。

```sh
npm ci --prefix web && npm run build --prefix web
./scripts/snapshot-demo.sh complete
```

打开终端提示的本机地址，用临时演示口令进入。按 Ctrl+C 后，以 `unpriced` 替代 `complete` 再运行，比较缺价结果。脚本使用独立临时库，退出后清理；不会读取本地凭据。两种场景的自动验收：`./scripts/snapshot-demo.sh verify`。

## 运行 UI 原型

在 Bifrost 项目目录中执行：

```sh
cd ui-preview
npm ci
npm run dev
```

打开终端显示的本地地址。运行 `npm run build` 可进行 TypeScript 检查并重新生成离线看板。预览不需要币安凭据或 PostgreSQL。

正式系统选用 Go、PostgreSQL、React、TypeScript、Ant Design 和 ECharts。按首版设计，Go 将执行每 15 分钟一次的采集和业务计算，PostgreSQL 只存数据、不建外键。详见[首版技术栈](./Docs/首版技术栈.md)。
