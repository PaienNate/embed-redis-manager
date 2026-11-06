# 已知问题 / Known Issues

> 本库是把 AnotherRedisDesktopManager（ARDM）内嵌成 Go 依赖库的实现。本文记录**设计上明确不做的部分**、**已知限制**与**尚未处理的打磨项**，供接入方与后续维护参考。核心功能（连接注入、key 浏览/搜索、string/hash/list/set/zset 查看编辑、CLI、stream/rejson/TS/vector、cluster 节点、监控、导出导入、SockJS 流式、可插拔鉴权、IE11 legacy 构建）均已实现并通过测试。
>
> 最近更新：2026-11-06

## 一、设计上明确不做（by design）

- **内置登录鉴权**：鉴权交给宿主的 `Config.Middleware`（中间件包裹 prefix 下全部路由）。默认无鉴权，接入生产环境务必自行加。README 的示例用 cookie/session，因为浏览器页面导航无法带 `Authorization` 头。
- **"新建连接"入口**：内嵌版只列出宿主注入的 go-redis client；连接建立与生命周期归宿主。
- **外部脚本自定义 viewer**：原 ARDM 的 `ViewerCustom`（在宿主机 `spawn` 任意外部命令）已移除——在内嵌 Web 服务里是严重的安全漏洞。只保留内置 JS viewer。
- **Brotli / Gzip / Deflate / DeflateRaw viewer**：依赖 Node `zlib`，浏览器无法直接用，模块价值低，已移除。
- **专用 open/close 端点**：宿主注入的是已实例化、始终就绪的 client，前端"打开/关闭"只是本地创建/销毁代理。
- **WebSocket / SSE 流式主通道**：改用 SockJS 单通道（自动在 WebSocket→XHR-streaming→XHR-polling 间降级），适配受限网络。
- **设置页的 Font Family 选择**：浏览器无法枚举系统字体，只有固定几个选项、价值低，已从设置页隐藏（默认字体栈仍生效）。
- **设置页的"检查更新/手动下载"**：内嵌版没有 Electron 自动更新，已移除。
- **不迁移到 iconify**：图标仍用 Font Awesome 4.7（已做**字体子集化**，见下）。原因：IE11 不支持 CSS mask（iconify 常用的 `currentColor` 方案），且 key 树的 chevron 由第三方组件用 FA class 渲染，替换成本高。

## 二、已知限制 / 边界

- **MONITOR 仅支持单节点 `*redis.Client`**：v9 adapter 用“专用 client + 粘性连接”，v8 adapter 用原生 RESP2 连接自己实现（go-redis v8 没有现成 helper）。`*redis.ClusterClient` / `*redis.Ring` 仍返回 `ErrMonitorUnsupported`（pub/sub 订阅可用）。
- **Go 版本与 go-redis 版本绑定**：库本体要求 **Go 1.23+**；v9 适配器钉 `go-redis v9.18.x`（v9.19+ 要 Go 1.24，v9.23 要 Go 1.26）。宿主如果自行升级 go-redis，MVS 会选高版本，Go 下限随之提高；v8 适配器用 `github.com/go-redis/redis/v8 v8.11.5`。
- **危险命令未拦截**：`POST /command` 可执行 `FLUSHALL`/`DEBUG`/`SHUTDOWN` 等。安全边界是宿主鉴权；如需只读，宿主在 Middleware 里过滤，或后续考虑加 `Config.ReadOnly`/命令白名单。
- **阻塞命令（`BLPOP key 0` 等）**：走 HTTP 端点会在 go-redis 读超时后报错；如需阻塞命令请用其它通道。
- **db>0 时的专用 client**：为规避 go-redis 连接池 SELECT 不粘连，db≠0 的命令在**每 db 懒创建+缓存的专用 client** 上执行（由 v8/v9 适配器从 `Options()` 克隆）。它通过 `Options()` 继承了连接的 `Addr/Network/Dialer/TLS/Username/Password/Protocol/PoolSize/超时` 等；但 `Options()` 里没有 **hooks**（go-redis 无读取已注册 hook 的公开接口）、也没有 **哨兵/故障转移拓扑**（`FailoverOptions` 不可从 client 还原），所以该专用 client **不继承宿主的 `AddHook`**、failover 后地址可能钉死。负数的 `SELECT` 不再创建/缓存专用 client（交给服务器拒绝）。v9 适配器给派生 client 设置独立的 `MaintNotificationsConfig{Mode: disabled}`，既避免共享宿主配置指针的竞态，也避免每个派生 client 重复注册全局维护通知 handler。如需保留 hooks/failover，建议宿主**按 DB 分别注入 client**（多个 Connection）。
- **会话态命令**（`AUTH` / `CLIENT SETNAME` 等）仍走宿主注入的共享连接池，可能影响池内其它连接。
- **CSRF**：鉴权归宿主；命令端点强制 `application/json` 请求体，天然触发 CORS 预检，已挡掉简单跨站 POST。
- **无自动化 CI**：真实 Redis 的 stream/pub-sub 集成测试在无 Redis 环境下静默 SKIP（用 `REDISUI_TEST_REDIS_ADDR` 指定）。建议 CI 带 Redis service 跑全量。
- **前端无组件级单测**：`vitest` 只覆盖 `HttpRedisClient`/`stream.js` 的协议映射；`DeleteBatch`/`CliTab` 等组件行为靠走查。
- **vite 构建非确定性**：同一源码两次 `npm run build` 产物 hash 可能不同（不影响功能）。
- **前端运行时依赖**：`axios@0.27`（走 XHR，IE11 无需 fetch polyfill，带 30s 超时）、`CodeMirror 5`（替代 Monaco，IE11 兼容）。
- **IE11**：通过 `@vitejs/plugin-legacy` 产出 ES5 legacy bundle（`index-legacy-*.js` + `polyfills-legacy-*.js`）+ `url-polyfill` / `resize-observer-polyfill`；但 CSS 变量（连接颜色）与 flex `gap` 在 IE11 不生效（仅影响观感），建议在真实 IE11 冒烟测试（大 key 列表、表格、编辑器）。
- **产物体积**：现代 bundle ≈ 2.2 MB（gzip ≈ 670 KB），legacy ≈ 2.4 MB（gzip ≈ 682 KB）。Monaco 已移除（原来占 ~41%）；Font Awesome 已子集化，字体从 ≈930 KB 降到 ≈3 KB（woff2）/ 4 KB（woff）/ 6 KB（ttf）。

## 三、尚未处理的打磨项

- `redisui/command.go` 对**裸 JSON `null`** 参数会静默解成空串（前端不发 `null`，无实际影响）。
- 连接**拖拽排序**会写入 localStorage，但下次刷新被后端连接列表覆盖；嵌入模式下排序不持久（无后端接口）。
- 搜索模式下 scan 页块（count=10000）与暂停阈值（keysPageSize=500）不齐，首块可一次吐 up to 10000 再暂停。
- Status 集群表格的 Keys/Expires/TTL 列展示的是 slots/flags（值为 0），这几列排序为空操作、语义略混淆。
- cluster/ring 的连接 tooltip 只展示**第一个**节点地址（`Options().Addrs[0]`），不是完整节点列表。
- `command_test.go` 与 `integration_test.go` 的测试 helper 部分重复（可复用 `commandServer`）。

## 附：近期已修复（从清单移除）

- 核心不再直接依赖 go-redis：新增叶子接口包 `redisui/redisclient`、v9 适配器 `redisui/goredisv9`、v8 适配器 `redisui/goredisv8`；`go.mod` 降到 `go 1.23.0` + go-redis v9.18.0，`Connection.Client` 类型改为 `redisui.Client`（宿主用 `goredisv9.New` / `goredisv8.New` 包一层）。
- v9 MONITOR 改为“专用 client + 粘性连接”，不再依赖 v9.23 的 `EphemeralConn`；同时禁用派生 client 的维护通知，修复 `-race` 下 `bufio.Reader` 的竞态。
- v8 适配器补上 MONITOR：用 `Options()` 的 Dialer/TLS/Username/Password/DB 开专用 RESP2 连接，发 AUTH/SELECT/MONITOR 并逐行读取；不再对单节点返回 `ErrMonitorUnsupported`（cluster/ring 除外）。
- 命令日志已接线（`setCommandLogSink` 转发每条命令到 `CommandLog`，ping 过滤）。
- 版本号由后端 `Config.Version` 注入，设置页显示；移除检查更新/手动下载，增加致谢链接。
- 连接 tooltip 的 host/port 由后端从注入 client 的 `Options().Addr` 派生（standalone/failover 用 `Addr`，cluster/ring 取首个地址），不再显示 `Host: undefined`。
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
- 替换 Monaco 为 CodeMirror（IE11 兼容、体积大减）；fetch 换 axios（XHR + 30s 超时）；新增 IE11 legacy 构建。
- **Font Awesome 字体子集化**：只保留实际使用的 29 个字形，字体 ≈930 KB → woff2 ≈3 KB / woff ≈4 KB / ttf ≈6 KB；模板仍是 `<i class="fa fa-*">`，`currentColor` 继承不变。用 `npm run subset-icons`（`scripts/subset-fontawesome.js`）在增删图标后重新生成。
- 页面缩放（Page Zoom）现在真正生效（通过 CSS `zoom` 应用到 `document.body`；之前是 Electron 专属的空实现，0.5–2.0 可调，刷新后保持）。
- CodeMirror 编辑器：补齐 `foldgutter` addon（折叠栏不再是空白条），并用 ResizeObserver 在容器尺寸变化/切 tab 后自动 `refresh()`，修复正文与光标错位。
