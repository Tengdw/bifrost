# Bifrost

Bifrost 是个人使用的币安多账号资产看板，用来查看各钱包的美元估值、历史持仓和每日盈亏。首版覆盖现货、资金、Simple Earn 活期与定期、普通 U 本位合约，以及资金钱包内直接持有的币安美股。

**第 02–09 票：** 支持绑定只读账号、换钥、停用、手动刷新四钱包、查看同批报价与仓位，以及多账号筛选。可用下方合成工作区复现；真实行情、权限及账户模式尚未实盘验收。

**第 01 票：** 第 01 票提供本机合成持仓演示（Go、PostgreSQL、React），支持「最新采集」总览与同批明细。独立 UI 原型仍可离线查看；没有部署生产服务。

## 从哪里开始

| 目的 | 入口 |
|---|---|
| 了解首版范围、验收标准与实施顺序 | [系统总规格](./spec/SPEC.md) |
| 查阅 API、数据模型、估值口径和运维约定 | [专项文档索引](./Docs/README.md) |
| 体验账号切换、收益曲线及盈亏日历 | [离线看板](./ui-preview/dist/index.html) · [界面预览](./ui-preview/preview.png) |

## 运行多账号工作区

先安装并构建前端（见下节），再执行 `./scripts/workspace-demo.sh`。用终端提示的临时口令文件登录。绑定两个合成账号：API Key 分别为 `demo-a`、`demo-b`，示例 Secret 均为 `fixture-only`；A 的换钥示例为 `demo-a-rotated`。合成模式仅接受这三个示例 Key，不访问币安。

选择 A 并刷新四钱包，应显示 **8,794.40 USD**（6,000 + 99.80 + 499 + 2,195.60）；选择 A+B 一起刷新，应为 **9,044.40 USD**。查看持仓明细可核对数量、报价来源、时间与换算路径；仓位表里的 20,000 USD 名义价值不计入权益。只刷新现货时，所选账号完整总额未知。本机装有 Chrome 后运行 `./scripts/workspace-demo.sh verify-browser`，会在全新工作区绑定 A/B、刷新并核对浏览器金额。自动验收见[任务单](./.scratch/current-holdings/issues/)。

只读联调模式及配置文件见[部署与恢复](./Docs/部署与恢复.md)。定时采集、日终历史和盈亏仍属后续里程碑。

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
