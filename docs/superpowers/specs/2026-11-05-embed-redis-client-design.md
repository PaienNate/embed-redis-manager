# Embed Redis Client — 内嵌 Redis 管理页 Go 依赖库 设计说明

日期：2026-11-05
状态：设计稿（等待评审）

## 1. 背景与目标

`AnotherRedisDesktopManager`（下文称 ARDM）是一个 Electron + Vue2 + Element UI 的 Redis 桌面管理工具，本质是 Redis GUI 客户端。

本项目的目标：**把 ARDM 改造成一个可内嵌的 Go 依赖库**，让任意 Go 程序通过注册自己已经初始化好的 go-redis client，就能在自己的 Web 服务里获得一个 Redis 管理页面，用于在"Redis 难调试、无外网地址、内网账号权限不统一"等特殊环境下的数据库调整。

## 2. 关键决策（已与需求方对齐）

| 决策点 | 结论 |
| --- | --- |
| 功能范围 | 尽量完整（保留 ARDM 大部分功能；Core+全量按阶段补齐） |
| 客户端注入方式 | 宿主注入**已实例化的 go-redis client**（standalone / cluster / sentinel 均可） |
| UI 承载方式 | 静态 SPA + 同源 API，注册到同一个 `http.ServeMux` |
| 鉴权 | 提供**可插拔鉴权中间件**（宿主自实现） |
| Electron | **彻底移除**；去掉 `tunnel-ssh`、`electron remote`、`child_process` 等 node 专属依赖 |
| 前端构建产物 | **commit 进仓库**，宿主 `go build` 时用 `go:embed` 打进二进制，宿主无需安装 node |
| 实时流式通道 | **SockJS**（`sockjs-go` 服务端 + `sockjs-client` 前端），自动在 WebSocket → XHR-streaming → XHR-polling 间降级，适配政企/受限网络 |
| 自定义脚本查看器 | **废弃**，只保留内置 JS viewer；不引入 Go formatter 接口 |
| 前端构建方式 | **Vite + Vue2**（`@vitejs/plugin-vue2`），不沿用 ARDM 的 webpack4 |
| zlib 依赖 viewer | Brotli / Gzip / Deflate / DeflateRaw 一并去掉（依赖 node `zlib`，浏览器无法直接用，模块价值低） |

## 3. 对 ARDM 架构的关键洞察

ARDM **没有独立后端进程**：Redis 的实际读写由 `ioredis` 直接运行在 Electron 渲染进程里完成（`src/redisClient.js` 的 `createConnection` 返回 `new Redis(...)`，直连 Redis，甚至通过 `tunnel-ssh` 建 SSH 隧道）。Vue 组件通过 props 拿到这个 client 对象，调用 `client.call(cmd, ...)`、`client.callBuffer()`、`client.getBuffer()`、`client.hscanBufferStream()`、`client.nodes()` 等 ioredis 方法。

因此"把后端放到 Go 上"= 把"Redis 操作实际执行"这一层从浏览器里的 ioredis 挪到 Go HTTP 服务，Vue 前端改为通过 HTTP 访问 Go。

## 4. 核心设计：ioredis 兼容的 HTTP 客户端代理

为了在"尽量完整"下最小化前端改动，前端保留 ioredis 的**方法签名与返回值形状**，但底层改为 HTTP 转发：

- 新建前端模块 `HttpRedisClient`，复刻 ioredis 的关键接口：
  - 命令类：`call` / `callBuffer` / `getBuffer` 及具体方法代理（`get`/`set`/`del`/`hgetall`/`expire`/`xadd` 等）
  - 游标流式：`hscanBufferStream` / `sscanBufferStream` / `zscanBufferStream` 等（HTTP 转游标分页循环）
  - 集群：`nodes()`
  - 生命周期与事件：`status` / `on('ready')` / `on('error')` / `ping` / `quit` / `duplicate`
- 这些方法内部统一 `fetch`/`POST` 到 Go 端点的 `POST /api/redis/{connID}/command`。
- Go 端用 `client.Do(ctx, redis.NewCmd(args...))` 执行任意命令，把 RESP 回复序列化成 JSON（数字/字符串/数组/nil 保留类型；二进制走 base64），前端代理再还原为 ioredis 形状。

### 需要专门处理的三类情况

1. **游标流式**（`*scanBufferStream`）：ioredis 的 ScanStream 是一个 Node 流。改为前端代理里用 `SCAN/…` 的 `cursor` 做 `while` 分页循环，逐页请求 HTTP 端点，产出等价的异步可迭代结果。
2. **集群节点**（`nodes()`）：Go 端识别 `*redis.ClusterClient`，提供集群节点探测接口。
3. **二进制 / 大 value**：`callBuffer`/`getBuffer` 返回原始字节，Go 回复用 base64 编码，前端还原成 JS `Buffer`。

## 5. 整体架构

```
宿主 Go 程序 ──调用──> redisui.Register(mux, Config)
                            │
        ┌───────────────────┴──────────────────┐
        │  库内 (go:embed 打包的 Vue 前端)        │
        │  GET  {prefix}/                 SPA 入口 │
        │  GET  {prefix}/api/connections  列出宿主注入的连接 │
        │  POST {prefix}/api/redis/{connID}/command  泛化命令 │
        │  … open/close/ping/dbsize/scan             │
        │  /api/redis/{connID}/stream   SockJS 流通道 │
        └───────────────────┬──────────────────┘
                            │ go-redis（通用命令/类型探测）
                    宿主注入的 *redis.Client / ClusterClient
```

### 连接模型

- 宿主通过 `Config.Connections` 注入，每条连接含 `ID`、`Name`、`Client`（go-redis）、`Enabled`。
- 内嵌版**不再有"新建连接"弹窗**：`Aside` 连接菜单改为渲染后端返回的连接列表。
- 连接开关 `Enabled` 决定该连接是否出现在 UI；库级配置决定整个管理页是否挂载。

### 鉴权

- `Config.Middleware func(http.Handler) http.Handler` 包裹全部路由，宿主自实现（token / 登录校验 / 网关）。
- 库内不做内置登录。

### 实时流式通道（SockJS）

- 一个 SockJS 通道服务 MONITOR 和 pub/sub（SUBSCRIBE / PSUBSCRIBE）。
- Go 服务端用 `sockjs-go`，前端用 `sockjs-client`。
- 进入 subscribe / monitor 后（与 ARDM 一致）输入框禁用，纯服务端→客户端推送。

## 6. API 面（库对宿主暴露 / 库对前端暴露）

### 库对宿主暴露

```go
package redisui

type Connection struct {
    ID      string
    Name    string
    Client  redis.UniversalClient // 支持 *redis.Client / *redis.ClusterClient 等
    Enabled bool
}

type Config struct {
    Prefix      string                       // 路由前缀，默认 /redisui
    Connections []Connection
    Middleware  func(http.Handler) http.Handler
}

func Register(mux *http.ServeMux, cfg Config) error
```

### 库对前端暴露（挂在 `{prefix}` 下）

- `GET  {prefix}/`：SPA 入口（index.html + 静态资源）
- `GET  {prefix}/api/connections`：列出注入的连接
- `POST {prefix}/api/redis/{connID}/command`：执行任意命令（body：`{cmd, args}`）
- `POST {prefix}/api/redis/{connID}/open` / `/close`：打开 / 关闭
- `POST {prefix}/api/redis/{connID}/ping`：健康检查
- `POST {prefix}/api/redis/{connID}/dbsize`：库大小
- `GET  {prefix}/api/redis/{connID}/scan`：`cursor/match/count` 游标扫描
- `GET  {prefix}/api/redis/{connID}/cluster/nodes`：集群节点
- `GET  {prefix}/api/redis/{connID}/stream`：SockJS 流通道（MONITOR / pub-sub）

## 7. 数据流示例（查看 string 值）

前端 KeyList 点击 key → `HttpRedisClient.call('get', key)` → `POST /command` → Go `client.Do` 执行 → 序列化返回 → 前端渲染。

## 8. 错误处理

- `client.Do` 的 Redis 错误包成统一响应 `{error, reply}`，前端代理 reject，保持与 ioredis 一致的 promise 语义。
- 连接未注入 / 未启用 → `404`；命令超时走 `context` 超时。
- 未知命令 / 参数错误 → 返回 Redis 原始错误信息。

## 9. 前端改动要点

- `src/redisClient.js`：`createConnection` 返回 `HttpRedisClient` 代理（绑定 `connID`），不再 `new Redis` / `createTunnel`。
- 连接列表来源改为后端 `/api/connections`；移除"新建连接"对话框。
- 移除所有 `electron` `remote` / `ipcRenderer` / `child_process` / `fs` / `tunnel-ssh` 依赖：
  - 文件读取（TLS / SSH 私钥）不再需要（连接由宿主建好）。
  - `ViewerCustom.vue` 及"Custom viewer"入口删除。
- 流式（MONITOR / pub-sub）改用 `sockjs-client`。
- `storage.js` 等浏览器 localStorage 逻辑可保留。

### 内置 viewer 保留

json / msgpack / protobuf / hex / binary / php / pickle / java serialize 等（纯客户端 JS 库）原样保留。移除"Custom（外部脚本）"这一项；同时移除依赖 node `zlib` 的 Brotli / Gzip / Deflate / DeflateRaw viewer。

## 10. 构建与嵌入

- 前端仓库内保留 `package.json` / webpack 构建（去掉 Electron 部分），产出静态文件到 `web/dist`。
- **构建产物 commit 进仓库**。
- Go 侧 `//go:embed web/dist` 打进二进制，宿主无需 node。

## 11. 测试策略

- **Go 单元测试**：命令序列化（类型保留 / 二进制 base64 / nil）。
- **Go 集成测试**：用 `github.com/alicebob/miniredis` 跑通用命令、SCAN 游标、各类型读写；不依赖真实 Redis。
- **集群 / 流式**：单独测试（cluster 节点、SockJS 通道事件）。
- **前端测试**：vitest 测 `HttpRedisClient` 的请求-回复映射、游标分页、二进制还原。
- **E2E**：起一个带注入 client 的示例 Go 服务，浏览器手工/脚本验证关键流程。

## 12. 落地顺序（分阶段，每阶段可验证）

1. **骨架**：库包 + 前端构建嵌入（`go:embed`）+ `Register(mux)` + 连接列表 API + 泛化命令端点（跑通一个 `get`）。
2. **前端代理**：`HttpRedisClient` + Key 浏览 / 搜索。
3. **常见类型查看 / 编辑**：string / hash / list / set / zset。
4. **CLI 命令框**（含 SockJS 的 MONITOR / pub-sub）。
5. **补齐"尽量完整"**：stream、cluster、sentinel、监控、导出导入、其余 viewer。
6. **鉴权中间件 + 打包 / 文档 / 集成测试收尾**。

## 13. 不在范围内 / 明确不做

- 不内嵌 Electron 桌面形态。
- 不做内置登录鉴权（交给宿主的中间件）。
- 不做"新建连接"入口（连接由宿主注入）。
- 不做外部脚本自定义 viewer（spawn 进程）及 `{HEX_FILE}` 临时文件机制。
- 不做 Brotli / Gzip / Deflate / DeflateRaw viewer（依赖 node `zlib`）及其它 node 原生能力。
- 不引入 WebSocket / SSE 作为流式主通道（用 SockJS 单通道覆盖并自动降级）。

## 14. 安全考量

- 移除 `child_process` / 外部脚本执行，避免浏览器触发宿主任意命令执行。
- 鉴权由宿主中间件保证；库默认尽量少暴露能力（未启用的连接返回 404）。
- 二进制 / 大值传输走 base64 + 大小上限保护。
