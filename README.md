# Bifrost

Bifrost 是个人使用的币安多账号资产看板，用来查看各钱包的美元估值、历史持仓和每日盈亏。首版覆盖现货、资金、Simple Earn 活期与定期、普通 U 本位合约，以及资金钱包内直接持有的币安美股。

**当前状态：** 已确定系统规格并完成可交互的 UI 原型。原型使用示例数据，可离线查看；正式 Go 服务、数据库迁移和生产部署尚未实现。

## 从哪里开始

| 目的 | 入口 |
|---|---|
| 了解首版范围、验收标准与实施顺序 | [系统总规格](./spec/SPEC.md) |
| 查阅 API、数据模型、估值口径和运维约定 | [专项文档索引](./Docs/README.md) |
| 体验账号切换、收益曲线及盈亏日历 | [离线看板](./ui-preview/dist/index.html) · [界面预览](./ui-preview/preview.png) |

## 运行 UI 原型

在 Bifrost 项目目录中执行：

```sh
cd ui-preview
npm ci
npm run dev
```

打开终端显示的本地地址。运行 `npm run build` 可进行 TypeScript 检查并重新生成离线看板。预览不需要币安凭据或 PostgreSQL。

正式系统选用 Go、PostgreSQL、React、TypeScript、Ant Design 和 ECharts。Go 执行每 15 分钟一次的采集和业务计算，PostgreSQL 只存数据、不建外键。详见[首版技术栈](./Docs/首版技术栈.md)。
