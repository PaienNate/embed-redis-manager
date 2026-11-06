# AGENTS.md

面向在本仓库工作的 AI 代理 / 开发者的约定与操作指南。开始改动前先读本文件和 `KNOWN_ISSUES.md`。

## 项目定位

把 AnotherRedisDesktopManager（ARDM）改造成**可内嵌的 Go 库** `embedRedisClient/redisui`：宿主注入已实例化的 go-redis client（用 `goredisv9.New` / `goredisv8.New` 适配），库在自己的 `http.ServeMux` 前缀下提供同源的 Redis 管理 SPA。前端（Vue2 + Element UI）用 Vite 构建并经 `go:embed` 打进二进制。核心包不直接依赖 go-redis，库本体要求 **Go 1.23+**。

## 目录结构

- `redisui/` —— Go 库（`redisui.go` 入口/路由、`command.go` 命令端点、`embed.go` 资源嵌入、`stream.go` SockJS、`cluster.go` 等）。
  - `redisui/redisclient/` —— 叶子接口包（`Client` / `Command` / `Subscription` / `Monitor` / `Kind`），核心和 adapter 都只依赖它，避免 import cycle。
  - `redisui/goredisv9/` —— go-redis v9 adapter（钉 `v9.18.0`；MONITOR 走专用 client）。
  - `redisui/goredisv8/` —— go-redis v8 adapter（`v8.11.5`；没有 MONITOR）。
- `redisui/web/` —— 前端源码（Vite + Vue2）与构建产物：
  - `src/` 源码；`src/assets/fontawesome-subset.*` 为生成的图标字体子集。
  - `scripts/subset-fontawesome.js` 图标子集生成脚本。
  - `dist/` **构建产物（必须提交，见下）**。
- `example/` —— 示例宿主程序。
- `docs/` —— 设计规格、实现计划、需求、英文 README。
- `KNOWN_ISSUES.md` —— 已知限制与待完善项。
- `reference/` —— 上游 ARDM 源码（**已 gitignore，不要提交**）。

## 构建与测试

```bash
cd redisui/web && npm install && npm run build   # 前端（需重新生成并提交 dist）
cd redisui/web && npx vitest run                 # 前端测试
cd redisui/web && npm run subset-icons           # 增删 fa-* 图标后重新生成子集
go build ./... && go test ./...                  # Go（测试用 miniredis；含 v8/v9 adapter）
go test -race ./...                              # 并发/竞态（MONITOR 改动必跑）
gofmt -l redisui/ example/                       # 必须为空
```

## 关键约定（容易踩坑）

1. **`redisui/web/dist` 是提交进仓库的构建产物**，`//go:embed` 在编译期读取它。**改前端后必须 `npm run build` 并一并提交 `dist`**，否则 Go 侧是旧页面。
2. **前端是 Vite + Vue2**：
   - 不要添加 `'vue$': 'vue/dist/vue.esm.js'` 这类 webpack 风格别名——Vite 的 alias 按前缀匹配、不认 `$`，会静默失效（曾导致空白页）。App 用 `render: h => h(App)` 挂载。
   - 浏览器全局 `Buffer/process/global` 由 `vite-plugin-node-polyfills` 注入。
3. **图标是 Font Awesome 子集**：模板仍用 `<i class="fa fa-*">`。新增/删除图标要改 `scripts/subset-fontawesome.js` 的 `ICONS`，跑 `npm run subset-icons` 再 `npm run build`；否则字形缺失。
4. **命令端点契约**（`POST {prefix}/api/redis/{connID}/command`）：
   - 请求 `{cmd, args:[{t:"s"|"b",v}], binary, db}`；回复 `{error, reply}`，reply 为带类型标签的 JSON（`s/i/a/n/b`）。
   - Redis 命令错误只 reject（子类），不要 `emit('error')`——否则会误关连接。网络/HTTP 错误才是连接级错误。
   - `db>0` 走按 db 缓存的专用 client；负数 SELECT 不缓存。
5. **IE11**：`@vitejs/plugin-legacy` 产出 ES5 legacy bundle + polyfill。不要依赖 CSS `mask`（IE11 不支持）；避免 flex `gap` 作为关键布局。
6. **测试/接口**：Go 测试统一用 `miniredis`；前端纯逻辑测试放 `redisui/web/test/`。改动命令/回复序列化时要同步更新 `redisui/command_test.go`。`redisui/redisui_test.go` 里的 `testRedis` 同时暴露原始 go-redis client（`Set`/`HSet` 等 seed 用）和 `redisui.Client` 接口，新增/修改 adapter 能力时记得同步它。
7. **样式**：Element UI 主题从 `static/theme/{light,dark}/index.css` 以 `<link>` 在应用 CSS **之前**加载；应用样式覆盖 Element 默认值依赖这个顺序。
8. **客户端适配层**：`Connection.Client` 是 `redisui.Client`（`redisui/redisclient` 的别名），不是 `redis.UniversalClient`。核心包**不要 import go-redis**；版本相关逻辑只放 `redisui/goredisv9` / `redisui/goredisv8`。宿主用 `goredisv9.New(c)` / `goredisv8.New(c)` 包一层。新增能力时先改 `redisclient.Client` 接口，再改两个 adapter 和测试 helper。
   - v9 MONITOR 用“专用 client + 粘性连接”；`Stop()` 里**只 `cli.Close()`，不要 `conn.Close()`**——后者会走 `pool.putConn -> HasBufferedData`，和 `readMonitor` 的 `bufio.Reader` 抢写，`-race` 必现。
   - 派生 client（db/MONITOR）用独立的 `MaintNotificationsConfig{Mode: disabled}`，不要直接共享宿主的 `Options` 指针。
   - v8 没有 MONITOR，`Monitor` 返回 `ErrMonitorUnsupported`；不要为它伪造。

## 提交规范

- 提交信息用**中文**，保留 Conventional Commits 前缀，例如：`fix(web): 修复 zset score 为 NaN`、`feat(redisui): 新增通用命令端点`。
- 提交作者统一为 `PaienNate <1101839859@qq.com>`。
- 一个逻辑改动一个提交；前端改动请把重建后的 `dist` 一起提交。
- 不要提交 `reference/`、`node_modules/`、`.idea/`。
