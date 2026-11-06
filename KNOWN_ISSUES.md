# 已知问题 / Known Issues

> 本库是把 AnotherRedisDesktopManager（ARDM）内嵌成 Go 依赖库的实现。本文记录**设计上明确不做的部分**、**已知限制**与**待完善项**，供接入方与后续维护参考。核心功能（连接注入、key 浏览/搜索、string/hash/list/set/zset 查看编辑、CLI、stream/rejson/TS/vector、cluster 节点、监控、导出导入、SockJS 流式、可插拔鉴权）均已实现并通过测试。

## 一、设计上明确不做（by design）

- **内置登录鉴权**：鉴权交给宿主的 `Config.Middleware`（中间件包裹 prefix 下全部路由）。默认无鉴权，接入生产环境务必自行加。
- **"新建连接"入口**：内嵌版只列出宿主注入的 go-redis client；连接建立与生命周期归宿主。
- **外部脚本自定义 viewer**：原 ARDM 的 `ViewerCustom`（在宿主机 `spawn` 任意外部命令）已移除——在内嵌 Web 服务里是严重的安全漏洞。只保留内置 JS viewer。
- **Brotli / Gzip / Deflate / DeflateRaw viewer**：依赖 Node `zlib`，浏览器无法直接用，模块价值低，已移除。
- **专用 open/close 端点**：宿主注入的是已实例化、始终就绪的 client，前端"打开/关闭"只是本地创建/销毁代理。
- **WebSocket / SSE 流式主通道**：改用 SockJS 单通道（自动在 WebSocket→XHR-streaming→XHR-polling 间降级），适配受限网络。

## 二、已知限制 / 边界

- **MONITOR 仅支持单节点 `*redis.Client`**：`*redis.ClusterClient` / `*redis.Ring` 不支持 MONITOR，会返回错误事件（pub/sub 订阅仍可用）。
- **危险命令未拦截**：`POST /command` 可执行 `FLUSHALL`/`DEBUG`/`SHUTDOWN` 等。安全边界是宿主鉴权；如需只读，宿主在 Middleware 里过滤，或后续考虑加 `Config.ReadOnly`/命令白名单。
- **阻塞命令（`BLPOP key 0` 等）**：走 HTTP 端点会在 go-redis 读超时后报错；如需阻塞命令请用其它通道。
- **db>0 时的专用 client**：为规避 go-redis 连接池 SELECT 不粘连，db≠0 的命令在**每 db 懒创建+缓存的专用 client** 上执行。代价：该专用 client **不继承宿主的 `AddHook` 钩子**；哨兵故障转移后其地址可能钉死在初始解析结果。（README §5 已记。）
- **会话态命令**（`AUTH` / `CLIENT SETNAME` 等）仍走宿主注入的共享连接池，可能影响池内其它连接。
- **CSRF**：鉴权归宿主；命令端点强制 `application/json` 请求体，天然触发 CORS 预检，已挡掉简单跨站 POST。
- **命令日志**：已重新接线——`HttpRedisClient` 通过 `setCommandLogSink` 把每条命令（含 CLI）转发到 `CommandLog`（`ping` 被过滤）。
- **无自动化 CI**：真实 Redis 的 stream/pub-sub 集成测试在无 Redis 环境下静默 SKIP（用 `REDISUI_TEST_REDIS_ADDR` 指定）。建议 CI 带 Redis service 跑全量。
- **前端无组件级单测**：`vitest` 只覆盖 `HttpRedisClient`/`stream.js` 的协议映射；`DeleteBatch`/`CliTab` 等组件行为靠走查。
- **vite 构建非确定性**：同一源码两次 `npm run build` 产物 hash 可能不同（不影响功能）。

## 三、待完善项（打磨，尚未修）

以下为最终审查判定为**打磨项、非阻断**的遗留问题，按影响排列：

### 健壮性 / 正确性
- `redisui/web/src/httpClient.js` 的 `duplicate()` 重建代理时不携带 `options`/`condition`，CLI 的 `anoClient` 默认 db0、与主 client 的 db 状态靠 bus 同步，可能不一致。
- `DeleteBatch.vue` 某块删除失败后，其余 worker 仍会继续消费剩余块（提示失败但删除仍在进行）；建议加中断标志。
- `stream.go` 的 `runStream` 在窄窗口下（Recv goroutine 已取到消息、runStream 恰因 Send 失败退出）可能 goroutine 泄漏。
- `redisui/command.go` 对**裸 JSON `null`** 参数会静默解成空串（前端不发 `null`，无实际影响）。
- 越界/负数的 `SELECT`（如 `-1`、`9999`）会路由到 db 专用 client，拨号时报错并缓存一个无用 client（仍报错、无状态污染）。

### 前端 UX / 装饰
- 连接 tooltip 显示 `Host: undefined`（后端连接 DTO 无 host/port 字段）。
- 连接颜色标签刷新后丢失（fetch 连接列表时未合并 localStorage 里的 color）。
- 设置页的 Export/Import connections 已无意义（连接来自后端，导出是空 bundle）。
- 连接拖拽排序写入 localStorage，但下次刷新被后端 fetch 覆盖，嵌入模式下无意义。
- 主题 `system` 实际等同 light；`appVersion` 取不到时显示空标签。
- 搜索模式下 scan 页块（count=10000）与暂停阈值（keysPageSize=500）不齐，首块可一次吐 up to 10000 再暂停。
- `cli` 的 `close()` 置 `_sock=null` 但不清 `_lastControl`（当前无实例复用路径，无实际影响）。

### 后端 / 集群
- cluster 节点 IPv6 地址解析：`splitClusterAddr` 用 `Cut(":")`，对 `[2001:db8::1]:6379@16379` 解析错误（仅影响展示）。
- Status 集群表格的 Keys/Expires/TTL 列全是 0（展示的是 slots/flags），这几列排序为空操作、语义略混淆。
- 每次 key 扫描都会发一次 `GET /cluster/nodes`（`nodes()` 现为真实请求），结果被丢弃，属多余请求。

### 测试
- `integration_test.go` 的 SCAN/DBSIZE 子测试依赖前置 SET/GET 写入，非自包含。
- `command_test.go` 与 `integration_test.go` 的测试 helper 部分重复（可复用 `commandServer`）。
- 缺"禁用连接 404""cluster 命令错误路径"的专项测试。

### 文档
- `README.md` 的鉴权示例用 `Authorization: Bearer` 头——浏览器页面导航不会带该头，照抄会把自己锁在门外；浏览器端请用 cookie/session 或由网关注入，README 已注明为"机制演示"。
