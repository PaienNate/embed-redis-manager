# 已知问题 / Known Issues

> 本库是把 AnotherRedisDesktopManager（ARDM）内嵌成 Go 依赖库的实现。本文记录**设计上明确不做的部分**、**已知限制**与**尚未处理的打磨项**，供接入方与后续维护参考。核心功能（连接注入、key 浏览/搜索、string/hash/list/set/zset 查看编辑、CLI、stream/rejson/TS/vector、cluster 节点、监控、导出导入、SockJS 流式、可插拔鉴权）均已实现并通过测试。

## 一、设计上明确不做（by design）

- **内置登录鉴权**：鉴权交给宿主的 `Config.Middleware`（中间件包裹 prefix 下全部路由）。默认无鉴权，接入生产环境务必自行加。
- **"新建连接"入口**：内嵌版只列出宿主注入的 go-redis client；连接建立与生命周期归宿主。
- **外部脚本自定义 viewer**：原 ARDM 的 `ViewerCustom`（在宿主机 `spawn` 任意外部命令）已移除——在内嵌 Web 服务里是严重的安全漏洞。只保留内置 JS viewer。
- **Brotli / Gzip / Deflate / DeflateRaw viewer**：依赖 Node `zlib`，浏览器无法直接用，模块价值低，已移除。
- **专用 open/close 端点**：宿主注入的是已实例化、始终就绪的 client，前端"打开/关闭"只是本地创建/销毁代理。
- **WebSocket / SSE 流式主通道**：改用 SockJS 单通道（自动在 WebSocket→XHR-streaming→XHR-polling 间降级），适配受限网络。
- **设置页的 Font Family 选择**：浏览器无法枚举系统字体，只有固定几个选项、价值低，已从设置页隐藏（默认字体栈仍生效）。

## 二、已知限制 / 边界

- **MONITOR 仅支持单节点 `*redis.Client`**：`*redis.ClusterClient` / `*redis.Ring` 不支持 MONITOR，会返回错误事件（pub/sub 订阅仍可用）。
- **危险命令未拦截**：`POST /command` 可执行 `FLUSHALL`/`DEBUG`/`SHUTDOWN` 等。安全边界是宿主鉴权；如需只读，宿主在 Middleware 里过滤，或后续考虑加 `Config.ReadOnly`/命令白名单。
- **阻塞命令（`BLPOP key 0` 等）**：走 HTTP 端点会在 go-redis 读超时后报错；如需阻塞命令请用其它通道。
- **db>0 时的专用 client**：为规避 go-redis 连接池 SELECT 不粘连，db≠0 的命令在**每 db 懒创建+缓存的专用 client** 上执行。代价：该专用 client **不继承宿主的 `AddHook` 钩子**；哨兵故障转移后其地址可能钉死在初始解析结果。（README §5 已记。）负数的 `SELECT` 不会再创建/缓存专用 client（交给服务器拒绝）。
- **会话态命令**（`AUTH` / `CLIENT SETNAME` 等）仍走宿主注入的共享连接池，可能影响池内其它连接。
- **CSRF**：鉴权归宿主；命令端点强制 `application/json` 请求体，天然触发 CORS 预检，已挡掉简单跨站 POST。
- **无自动化 CI**：真实 Redis 的 stream/pub-sub 集成测试在无 Redis 环境下静默 SKIP（用 `REDISUI_TEST_REDIS_ADDR` 指定）。建议 CI 带 Redis service 跑全量。
- **前端无组件级单测**：`vitest` 只覆盖 `HttpRedisClient`/`stream.js` 的协议映射；`DeleteBatch`/`CliTab` 等组件行为靠走查。
- **vite 构建非确定性**：同一源码两次 `npm run build` 产物 hash 可能不同（不影响功能）。
- **IE11**：通过 `@vitejs/plugin-legacy` 产出 ES5 legacy bundle + 必要 polyfill；但 CSS 变量（连接颜色）与 flex `gap` 在 IE11 不生效（仅影响观感），建议在真实 IE11 冒烟测试。

## 三、尚未处理的打磨项

- `redisui/command.go` 对**裸 JSON `null`** 参数会静默解成空串（前端不发 `null`，无实际影响）。
- 连接**拖拽排序**会写入 localStorage，但下次刷新被后端连接列表覆盖；嵌入模式下排序不持久（无后端接口）。
- 搜索模式下 scan 页块（count=10000）与暂停阈值（keysPageSize=500）不齐，首块可一次吐 up to 10000 再暂停。
- Status 集群表格的 Keys/Expires/TTL 列展示的是 slots/flags（值为 0），这几列排序为空操作、语义略混淆。
- `command_test.go` 与 `integration_test.go` 的测试 helper 部分重复（可复用 `commandServer`）。

## 附：近期已修复（从清单移除）

- 命令日志已接线（`setCommandLogSink` 转发每条命令到 `CommandLog`，ping 过滤）。
- 版本号由后端 `Config.Version` 注入，设置页显示。
- 连接 tooltip 不再显示 `Host: undefined`（无 host/port 时省略）。
- 连接颜色改为按 connID 存 localStorage，刷新后可恢复。
- `duplicate()` 携带 db / connectionName，CLI 与主 client 的 db 状态一致。
- 批量删除某块失败后会中止其余 worker，避免"提示失败但仍在删除"。
- `stream.go` 用 done channel 收尾 Recv goroutine，消除窄窗口泄漏。
- cluster 节点 IPv6 地址解析修正（按最后一个 `:` 切分）。
- `nodes()` 结果短期缓存，避免每次 key 扫描都发 `/cluster/nodes`。
- 主题 `system` 现按 `prefers-color-scheme` 跟随系统亮/暗。
- CLI `close()` 清空 `_lastControl`，不会在重连后重放已停止的订阅。
- 集成测试 SCAN/DBSIZE 子测试自包含；新增"禁用连接各端点 404"测试。
- README 鉴权示例改为 cookie/session（浏览器页面导航无法带 Authorization 头）。
