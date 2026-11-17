# 测试部署配置

完整操作步骤见[部署指南](../Docs/部署指南.md)。Compose 只运行一个应用容器，数据库使用已存在的专用测试 PostgreSQL。

复制 `.env.example` 为 `.env`，填写内网地址、浏览器访问的 HTTPS origin 和配置目录绝对路径。配置目录内准备以下文件，均须能由容器用户 `10001:10001` 读取：

| 文件 | 内容 |
|---|---|
| `database-url` | 测试数据库 DSN，包含访问所需凭据；容器内不能使用宿主机的 `localhost` 指代远端数据库。 |
| `master-key` | 32 个随机字节的标准 Base64 文本，长期保存，供账号凭据加密。 |
| `session-token` | 至少 32 个字符的随机登录口令。 |
| `tls.crt`、`tls.key` | 与 `BIFROST_PUBLIC_ORIGIN` 主机名或 IP 匹配的证书链和私钥；浏览器信任签发方。 |

文件与密钥不进入构建上下文。配置目录以只读方式挂载；主密钥应独立备份。Linux 示例权限为目录归属 `10001:10001`、`0500`，文件同归属、`0400`。

从仓库根目录运行：

```sh
docker compose --env-file deploy/.env config --quiet
docker compose --env-file deploy/.env build app
docker compose --env-file deploy/.env run --rm app -mode synthetic -migrate
docker compose --env-file deploy/.env up -d app
docker compose --env-file deploy/.env logs --tail=100 app
```

浏览器使用 `BIFROST_PUBLIC_ORIGIN` 的地址访问；证书、origin 和端口必须一致。迁移命令必须与 `.env` 中运行模式相同。改为 `readonly` 联调时，选择另一个专用测试数据库，再显式执行 `-mode readonly -migrate`，合成库不能改作真实账户库。

镜像按[多阶段构建](https://docs.docker.com/build/building/multi-stage/)打包 Go 二进制与前端产物；[Compose 服务配置](https://docs.docker.com/reference/compose-file/services/)限制为非 root、只读根文件系统和单应用实例。构建标签用于复现源码版本；发布时记录实际构建出的镜像 ID，升级和回滚使用固定版本标签。
