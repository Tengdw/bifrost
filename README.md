# Bifrost

个人币安多账号资产看板。运行 `make dev` 打开正式前端，开发库使用 `bifrost_test`。首次启动在终端设置管理员账号和密码，随后可绑定 Passkey 与币安只读账号。开发数据从首次真实采集开始积累。浏览器地址为 http://localhost:5173 。

生产库使用 `bifrost`。从根目录的 `.env.example` 准备 `.env`，配置反代的 HTTPS 地址后执行 `make deploy`。环境依赖、数据库凭据格式及主密钥备份见[部署指南](./Docs/部署指南.md)。

## 运行多账号工作区

先安装并构建前端（见下节），再执行 `./scripts/workspace-demo.sh`。用 demo 账号和终端提示的临时密码文件登录。绑定两个合成账号：API Key 分别为 `demo-a`、`demo-b`，示例 Secret 均为 `fixture-only`；A 的换钥示例为 `demo-a-rotated`。合成模式仅接受这三个示例 Key，不访问币安。

选择 A 并刷新四钱包，应显示 **8,794.40 USD**（6,000 + 99.80 + 499 + 2,195.60）；选择 A+B 一起刷新，应为 **9,044.40 USD**。查看持仓明细可核对数量、报价来源、时间与换算路径；仓位表里的 20,000 USD 名义价值不计入权益。只刷新现货时，所选账号完整总额未知。本机装有 Chrome 后运行 `./scripts/workspace-demo.sh verify-browser`，会在全新工作区绑定 A/B、刷新并核对浏览器金额。自动验收见[任务单](./.scratch/current-holdings/issues/)。

只读联调模式及配置文件见[部署指南](./Docs/部署指南.md)；恢复限制见[部署与恢复](./Docs/部署与恢复.md)。盈亏需等资金流水覆盖后才能核算。

## 运行最新持仓演示

需要 Go 1.25.7、Node.js 22.13+、PostgreSQL 17 或 18 的本机服务端工具（工作目录无需运行数据库）。若没有 pg_config，将 PGBIN 指向 initdb 和 pg_ctl 所在目录。

```sh
npm ci --prefix web && npm run build --prefix web
./scripts/snapshot-demo.sh complete
```

打开终端提示的本机地址，用 demo 账号和临时密码进入。按 Ctrl+C 后，以 `unpriced` 替代 `complete` 再运行，比较缺价结果。脚本使用独立临时库，退出后清理；不会读取本地凭据。两种场景的自动验收：`./scripts/snapshot-demo.sh verify`。

## 运行 UI 原型

在 Bifrost 项目目录中执行：

```sh
cd ui-preview
npm ci
npm run dev
```

打开终端显示的本地地址。运行 `npm run build` 可进行 TypeScript 检查并重新生成离线看板。预览不需要币安凭据或 PostgreSQL。

正式系统选用 Go、PostgreSQL、React、TypeScript、Ant Design 和 ECharts。首版设计计划由 Go 每 15 分钟采集并执行业务计算，PostgreSQL 只存数据、不建外键；详见[首版技术栈](./Docs/首版技术栈.md)。
