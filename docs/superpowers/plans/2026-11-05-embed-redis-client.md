# Embed Redis Client 内嵌管理页库 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 subagent-driven-development（推荐）或 executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 把 AnotherRedisDesktopManager 改造成一个可内嵌的 Go 依赖库，宿主通过注册已实例化的 go-redis client，在自己的 `http.ServeMux` 上挂载一个同源 Redis 管理 SPA。

**架构：** 前端保留 ARDM 的 Vue2 UI，用一块"ioredis 兼容"的 `HttpRedisClient` 代理把 Redis 操作改成 HTTP 转发；Go 库用 `redis.NewCmd` 提供泛化命令端点，用 `go:embed` 打包前端产物，注册到一个 `http.ServeMux` 前缀下。流式（MONITOR/pub-sub）走 SockJS 单通道。

**技术栈：** Go 1.26、`github.com/redis/go-redis/v9`、`github.com/igm/sockjs-go/sockjs`；前端 Vue2 + Element UI + **Vite**（`@vitejs/plugin-vue2`）、`sockjs-client`；测试 `github.com/alicebob/miniredis/v2`、vitest。

**规格：** `docs/superpowers/specs/2026-11-05-embed-redis-client-design.md`（两份都要读，计划以规格为依据）。

## 全局约束

- 彻底移除 Electron：不依赖 `electron`、`tunnel-ssh`、`child_process`、`fs`、`net`、`tls`；webpack `target` 用 `web`。
- 连接由宿主注入已实例化的 go-redis client（`*.Client` / `*.ClusterClient`），库不建连接、不做"新建连接"。
- 前端构建产物 commit 进仓库并 `go:embed`，宿主无需 node。
- 流式只用 SockJS（服务端 `sockjs-go`，前端 `sockjs-client`），降级顺序 WebSocket→XHR-streaming→XHR-polling。
- 鉴权由宿主 `Middleware` 提供，库不做内置登录。
- 仅保留内置 JS viewer，废弃外部脚本自定义 viewer（`ViewerCustom.vue`）及 `{HEX_FILE}` 机制。
- 前端用 **Vite 新建**（`@vitejs/plugin-vue2`），不沿用 ARDM 的 webpack4；`require('zlib')` 相关的 Brotli/Gzip/Deflate/DeflateRaw viewer 一并去掉（少几个模块而已）。
- 回复序列化需保留类型（string/int/nil/array/status），二进制走 base64。

## 审查重点（Review Focus）

1. **`go:embed` 依赖提交的构建产物**——若 `redisui/web/dist` 缺失或未提交，`go build` 直接失败；需要一个测试/检查保证 dist 已被生成并提交。
2. **回复类型保真**——`get` 返回字符串、`incr` 返回整数、不存在的 key 返回 nil、`lrange` 返回数组、`set` 返回状态 OK；序列化丢失任一种类型都会让前端渲染错乱；用 miniredis 集成测试钉住每种类型。
3. **未注册/未启用的连接**——访问不存在或 `Enabled=false` 的连接应返回 404，不得 panic；对任意非法 `connID` 都要有测试。
4. **二进制/盲值经 base64 往返**——`getBuffer`/`callBuffer` 必须把任意字节经 base64 无损传回前端；测试用含 0x00/非 UTF-8 字节的值。
5. **SockJS 流式在降级链路可用**——MONITOR/pub-sub 推送在 XHR-polling 传输下也能把事件送到前端；集成测试验证事件流不丢。

---

### 任务 1：Go 库骨架 —— Config/Connection 类型与 Register

**文件：**
- 创建：`redisui/redisui.go`
- 创建：`redisui/redisui_test.go`
- 修改：`go.mod`（新增 go-redis v9、sockjs-go、miniredis 依赖）

- [ ] **步骤 1：编写失败的测试**（`redisui_test.go`）

```go
func TestRegisterAddsRoutesToMux(t *testing.T) {
    mux := http.NewServeMux()
    cfg := Config{Prefix: "/r", Connections: []Connection{
        {ID: "c1", Name: "主缓存", Client: miniredisClient(t), Enabled: true},
    }}
    if err := Register(mux, cfg); err != nil {
        t.Fatalf("Register error: %v", err)
    }
    // 前缀下的 API 路由能命中（用 httptest 请求 /r/api/connections 期望 200）
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./redisui -run TestRegisterAddsRoutesToMux -v`
预期：FAIL —— `undefined: Config` / `undefined: Register`

- [ ] **步骤 3：在 `redisui/redisui.go` 中实现类型与 `Register`**

```go
type Connection struct {
    ID      string
    Name    string
    Client  redis.UniversalClient
    Enabled bool
}
type Config struct {
    Prefix      string
    Connections []Connection
    Middleware  func(http.Handler) http.Handler
}
func Register(mux *http.ServeMux, cfg Config) error
```
做法：`Prefix` 默认 `/redisui`；把连接按 `ID` 建索引存到内部 `server` 结构；`Register` 把后续各任务的处理器挂到 `cfg.Prefix` 前缀下（本任务先挂占位 `GET {prefix}/api/connections` 返回连接列表 JSON）。`Middleware` 非空则用它包裹。

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./redisui -run TestRegisterAddsRoutesToMux -v`
预期：PASS

- [ ] **步骤 5：Commit**

```bash
git add go.mod go.sum redisui/redisui.go redisui/redisui_test.go
git commit -m "feat(redisui): add Config/Connection types and Register"
```

### 任务 2：泛化命令端点（`POST /api/redis/{connID}/command`）

**文件：**
- 创建：`redisui/command.go`
- 创建：`redisui/command_test.go`

- [ ] **步骤 1：编写失败的测试**（用 miniredis，覆盖 string/int/nil/array/status 五种类型）

```go
func TestCommandExecutesViaUniversalClient(t *testing.T) { /* get 返回字符串 */ }
func TestCommandIntReplyPreserved(t *testing.T) { /* incr 返回整数 */ }
func TestCommandNilReplyPreserved(t *testing.T) { /* 不存在的 key get 返回 nil */ }
func TestCommandArrayReplyPreserved(t *testing.T) { /* lpush+lrange 返回数组 */ }
func TestCommandBinaryReplyBase64(t *testing.T) { /* 设置含 0x00 字节，callBuffer 取回 base64 一致 */ }
func TestCommandBadConnIDReturns404(t *testing.T) { /* 随机 connID → 404，不 panic */ }
func TestCommandDisabledConnReturns404(t *testing.T) { /* Enabled=false → 404 */ }
```
请求体 `{ "cmd": "get", "args": ["k"], "binary": false }`；响应体统一 `{ "error": string|null, "reply": <tagged> }`。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./redisui -run 'TestCommand' -v`
预期：FAIL —— 处理器未实现

- [ ] **步骤 3：在 `redisui/command.go` 中实现命令端点与回复序列化**

签名：`func (s *server) handleCommand(w http.ResponseWriter, r *http.Request)`；用 `redis.NewCmd(args...)` + `client.Do(ctx, cmd)` 执行。修复序列化采用带类型标签的 JSON：
```
{ "t":"s","v":"..." } | { "t":"i","v":123 } | { "t":"a","v":[...] } | { "t":"n" } | { "t":"status","v":"OK" } | { "t":"b","v":"<base64>" }
```
规则：`binary=true` 时 bulk 字符串/数组元素按 base64 编成 `t:"b"`；否则按 Go 返回类型映射到 `s/i/a/n/status`；`*redis.Cmd` 出错时 `error` 字段填 Redis 错误消息、`reply` 置 null。连接不存在或 `!Enabled` → `404`。

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./redisui -run 'TestCommand' -v`
预期：PASS

- [ ] **步骤 5：Commit**

```bash
git add redisui/command.go redisui/command_test.go
git commit -m "feat(redisui): add generic command endpoint with typed replies"
```

### 任务 3：新建 Vite + Vue2 前端并产出 dist

**文件：**
- 创建：`redisui/web/package.json`（`vue`、`element-ui`、`vue-i18n`、`@qii404/json-bigint`、`@qii404/redis-splitargs`、`@qii404/vue-easy-tree`、`algo-msgpack-with-bigint`、`java-object-serialization`、`php-serialize`、`pickleparser`、`protobufjs`、`rawproto`、`monaco-editor`、`vxe-table`、`keymaster`、`getopts`、`sortablejs`、`font-awesome`、`node-version-compare`、`sockjs-client`；devDeps：`vite`、`@vitejs/plugin-vue2`）
- 创建：`redisui/web/vite.config.js`（`@vitejs/plugin-vue2`；`resolve.alias` 配 `@`→`src`、`vue$`→`vue/dist/vue.esm.js`；`build.outDir='dist'`；`optimizeDeps` 包含 CJS 依赖）
- 创建：`redisui/web/index.html`（SPA 入口，挂 `#app`）
- 创建：`redisui/web/src/`（从 `reference/src` 复制并清理，见步骤 1）
- 创建：`redisui/web/dist/`（构建产物，**必须 commit**；理由：`//go:embed` 模式相对 .go 文件所在目录，放这里才能被 `redisui/embed.go` 的 `//go:embed web/dist` 引用）

- [ ] **步骤 1：新建 Vite 骨架并复制、清理 ARDM `src`**

用 Vite 建 Vue2 骨架（`package.json` + `vite.config.js` + `index.html` + `src/main.js`），把 `reference/src` 源码复制进 `redisui/web/src/`。清理项：
- 删除所有 `electron`/`remote`/`ipcRenderer`/`child_process`/`tunnel-ssh`/`fs`/`readline`/`path` 引用（`redisClient.js`、`shortcut.js`、`addon.js`、`Setting.vue`、`KeyListVirtualTree.vue`、`ConnectionMenu.vue`、`ViewerProtobuf.vue`、`ViewerCustom.vue` 等）。
- 删除 `ViewerCustom.vue`；删除依赖 node `zlib` 的 `ViewerBrotli.vue`、`ViewerGzip.vue`、`ViewerDeflate.vue`、`ViewerDeflateRaw.vue`（按约定去掉这几个模块）。
- 把 `require('@qii404/json-bigint')`、`require('protobufjs/minimal')` 改为 `import`。
- `main.js` 里 `process.on('uncaughtException')` 等 node 调用移除或做浏览器适配。

- [ ] **步骤 2：执行构建**

运行：`cd redisui/web && npm install && npm run build`
预期：`redisui/web/dist/index.html` + `redisui/web/dist/assets/...` 生成，无报错。

- [ ] **步骤 3：验证可访问性（跑一次）**

用 `python3 -m http.server` 起在 `redisui/web/dist`，浏览器打开确认 SPA 正常渲染（前后端未联调，连接列表暂时为空属预期）。

- [ ] **步骤 4：Commit**

```bash
git add redisui/web/ && git commit -m "build(web): scaffold Vite+Vue2 frontend from ARDM src"
```

### 任务 4：Go 静态资源嵌入与连接列表联调

**文件：**
- 创建：`redisui/embed.go`（含 `//go:embed all:web/dist` 的 embed 变量）
- 修改：`redisui/redisui.go`、`redisui/redisui_test.go`
- 创建：`redisui/embed_test.go`

- [ ] **步骤 1：编写失败的测试**

```go
func TestEmbeddedDistServesIndex(t *testing.T) { /* GET {prefix}/ 返回 200 且 body 含 <div id="app"> */ }
func TestStaticAssetServed(t *testing.T) { /* GET {prefix}/static/js/*.js 返回 200，Content-Type 正确 */ }
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./redisui -run TestEmbedded -v`
预期：FAIL —— embed 变量/处理器未实现

- [ ] **步骤 3：实现 embed 与静态文件服务**

`redisui/embed.go` 用 `//go:embed all:web/dist` 声明 `var distFS embed.FS`；`redisui.go` 的 `server` 增加 `frontend fs.FS`，用 `http.FileServerFS`（或用 `http.FileServer` 包一层）挂到 `{prefix}/`，并处理 SPA 的 history 回落到 `index.html`。因为 `redisui/embed.go` 与 `redisui/web/dist` 同仓库层级，`//go:embed web/dist` 可直接命中。

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./redisui -run TestEmbedded -v`
预期：PASS

- [ ] **步骤 5：Commit**

```bash
git add redisui/embed.go redisui/embed_test.go redisui/redisui.go
git commit -m "feat(redisui): embed and serve frontend dist at prefix"
```

### 任务 5：前端 `HttpRedisClient` 代理

**文件：**
- 创建：`redisui/web/src/httpClient.js`（`HttpRedisClient` 类）
- 创建：`redisui/web/test/httpClient.spec.js`（vitest）
- 修改：`redisui/web/package.json`（加 `vitest` devDependency，加 `test` 脚本）

- [ ] **步骤 1：编写失败的测试**

用 vitest + mock `fetch`：
```js
test('call sends cmd+args and returns string', async () => { /* mock 返回 {error:null,reply:{t:'s',v:'ok'}} → 得到 'ok' */ });
test('callBuffer decodes base64 to Buffer', async () => { /* reply{t:'b'} → Buffer 字节相等 */ });
test('hscanBufferStream iterates cursor pages', async () => { /* mock 两页 cursor 循环，收集全部 key */ });
test('call rejects on error reply', async () => { /* reply.error → promise reject */ });
```

- [ ] **步骤 2：运行测试验证失败**

运行：`cd redisui/web && npx vitest run test/httpClient.spec.js`
预期：FAIL —— `HttpRedisClient` 未定义

- [ ] **步骤 3：在 `redisui/web/src/httpClient.js` 实现 `HttpRedisClient`**

签名：`constructor(base, connID)`；方法 `call(cmd, ...args)`、`callBuffer`、`getBuffer`、`scan(cursor,match,count)`、`nodes()`、`ping()`、`quit()`、`duplicate()`、`on(event,fn)`、`status`。类型映射规则见任务 2；`*scanBufferStream` 返回一个实现 `[Symbol.asyncIterator]` 的对象，内部用 `SCAN` 游标 `while` 循环逐页请求。

- [ ] **步骤 4：运行测试验证通过**

运行：`cd redisui/web && npx vitest run test/httpClient.spec.js`
预期：PASS

- [ ] **步骤 5：Commit**

```bash
git add redisui/web/src/httpClient.js redisui/web/test/httpClient.spec.js redisui/web/package.json redisui/web/package-lock.json
git commit -m "feat(web): add ioredis-compatible HttpRedisClient proxy"
```

### 任务 6：前端改用代理 —— 连接列表与打开/关闭

**文件：**
- 修改：`redisui/web/src/redisClient.js`（`createConnection` 返回 `HttpRedisClient`，绑定 `connID`，不再 `new Redis`/`createTunnel`）
- 修改：`redisui/web/src/components/ConnectionWrapper.vue`（从后端 `GET /api/connections` 取连接，调用 `open`/`close`/`ping`，`client.status` 改为代理提供的值）
- 修改：`redisui/web/src/App.vue` / `Aside.vue`（连接来源改为后端列表，移除新建连接入口）
- 修改：`redisui/web/src/components/NewConnectionDialog.vue`（删除或不再使用）

- [ ] **步骤 1：改写 `redisClient.js` 的 `createConnection`**

删除 `import Redis from 'ioredis'`、`tunnel-ssh`、`remote`；`createConnection(connID)` 返回 `new HttpRedisClient(base, connID)`。

- [ ] **步骤 2：改连接列表来源**

`App.vue`/`Aside.vue` 挂载时 `fetch('{prefix}/api/connections')` 填充连接菜单；`ConnectionWrapper` 收到 `connID` 后 `POST open`、`startPingInterval` 用代理 `ping`。

- [ ] **步骤 3：编译并手工联调**

运行：`cd redisui/web && npm run build` 后在宿主示例里注册一个真实/本地 redis，能打开连接并看到 key 列表（值渲染若未完成可先留空）。

- [ ] **步骤 4：Commit**

```bash
git add redisui/web/src/redisClient.js redisui/web/src/components/ConnectionWrapper.vue redisui/web/src/App.vue redisui/web/src/Aside.vue redisui/web/src/components/NewConnectionDialog.vue
git commit -m "feat(web): use HttpRedisClient and backend connection list"
```

### 任务 7：Key 浏览与搜索

**文件：**
- 修改：`redisui/web/src/components/KeyList.vue`、`KeyListNormal.vue`、`KeyListVirtualTree.vue`、`OperateItem.vue`
- 修改/新增：`redisui/scan.go` + `redisui/scan_test.go`（如需专门 scan/dbsize 端点）

- [ ] **步骤 1：后端补 `ping`/`dbsize`/`scan` 端点（若走专用端点）**

`redisui/scan.go` 提供 `GET {prefix}/api/redis/{connID}/scan?cursor=&match=&count=`；写 miniredis 测试覆盖游标翻页与多匹配。

- [ ] **步骤 2：改 KeyList/OperateItem 用代理**

将原先 `client.scan`/`client.dbsize` 等 ioredis 调用改为代理方法（内部已有 `scan`/`dbsize` 透传策略）。

- [ ] **步骤 3：编译 + 联调**

运行：`cd redisui/web && npm run build`，起宿主 Redis，确认 key 树/列表、搜索、双击展开、翻页正常。

- [ ] **步骤 4：Commit**

```bash
git add redisui/scan.go redisui/scan_test.go redisui/web/src/components/KeyList*.vue redisui/web/src/components/OperateItem.vue
git commit -m "feat(redisui,web): key browse, search and dbsize"
```

### 任务 8：常见类型查看/编辑（string/hash/list/set/zset）

**文件：**
- 修改：`redisui/web/src/components/contents/KeyContentString.vue`、`KeyContentHash.vue`、`KeyContentList.vue`、`KeyContentSet.vue`、`KeyContentZset.vue`
- 修改：`redisui/web/src/components/KeyHeader.vue`、`FormatViewer.vue`（保留内置 viewer，去外部脚本；Brotli/Gzip/Deflate/DeflateRaw 已在任务 3 删除，不再出现）
- 调整：`redisui/command.go`（需要时补充 bulk/base64 与错误分支测试）

- [ ] **步骤 1：逐个类型接通代理**

各 `KeyContent*.vue` 里 `client.getBuffer`/`hgetall`/`lrange`/`sadd`/`zadd`/`expire`/`ttl` 等改为 `HttpRedisClient` 对应方法（方法名与 ioredis 一致，代理内部映射命令）。

- [ ] **步骤 2：编辑/增删改联调**

确认值查看、二进制查看器、编辑保存、删除、TTL 修改在 string/hash/list/set/zset 上可用。

- [ ] **步骤 3：编译 + 联调 + commit**

运行：`cd redisui/web && npm run build`，对五种类型逐一手工验证。
```bash
git add redisui/web/src/components/contents redisui/web/src/components/KeyHeader.vue redisui/web/src/components/FormatViewer.vue
git commit -m "feat(web): view and edit string/hash/list/set/zset"
```

### 任务 9：CLI 命令框（含 SockJS MONITOR / pub-sub）

**文件：**
- 修改：`redisui/web/src/components/CliTab.vue`（`monitor`/`subscribe` 改走 SockJS；普通命令走 `call`）
- 创建：`redisui/stream.go` + `redisui/stream_test.go`（SockJS 服务端）
- 创建：`redisui/web/src/stream.js`（`sockjs-client` 封装）

- [ ] **步骤 1：实现 SockJS 流通道**

`redisui/stream.go` 用 `sockjs-go` 提供 `GET {prefix}/api/redis/{connID}/stream`；收到 `MONITOR`/`SUBSCRIBE`/`PSUBSCRIBE` 命令后在 Go 端建立对应订阅，把事件经 SockJS session 推送。miniredis 对 monitor/pub-sub 支持有限，用真实 Redis 做集成测试或对事件封装做单元测试。

- [ ] **步骤 2：前端接入 `sockjs-client`**

`redisui/web/src/stream.js` 用 `sockjs-client` 连接通道；`CliTab.vue` 在 monitor/subscribe 模式下把收到的命令/消息渲染到输出区。

- [ ] **步骤 3：编译 + 联调 + commit**

运行：`cd redisui/web && npm run build`，在受限/常规网络各验证一次降级链路。
```bash
git add redisui/stream.go redisui/stream_test.go redisui/web/src/stream.js redisui/web/src/components/CliTab.vue
git commit -m "feat(redisui,web): CLI console with SockJS monitor/subscribe"
```

### 任务 10：补齐"尽量完整"（stream/cluster/sentinel/监控/导出导入/其余 viewer）

**文件：**
- 修改：`redisui/command.go`、新增 `redisui/cluster.go`（`nodes()` 集群节点接口，识别 `*redis.ClusterClient`）
- 修改：`redisui/web/src/components/contents/KeyContentStream.vue`、`KeyContentReJson.vue`、`KeyContentTimeSeries.vue`、`KeyContentVector.vue`、`KeyContentArray.vue`、`Status.vue`、`MemoryAnalysis.vue`、`SlowLog.vue`、`viewers/*`
- 修改：`redisui/command.go`、`redisui/web/src/components/Export*`/导入相关组件

- [ ] **步骤 1：集群节点接口**

`redisui/cluster.go` 对 `*redis.ClusterClient` 暴露 `GET {prefix}/api/redis/{connID}/cluster/nodes`；测试用 cluster mock 或真实 cluster 验证。

- [ ] **步骤 2：stream/JSON/TimeSeries/Vector 组件接通**

把 `xadd`/`xrevrange`/`xlen` 等命令经代理透传，二进制/多字段正确序列化。

- [ ] **步骤 3：监控/内存分析/慢日志接入**

`Status.vue`、`MemoryAnalysis.vue`、`SlowLog.vue` 的 `info`/`config`/`slowlog`/自研分析命令经代理；监控走任务 9 的 SockJS 通道。

- [ ] **步骤 4：导出/导入接通**

导出/导入所依赖的批量命令（`dump`/`restore` 或遍历）经代理执行。

- [ ] **步骤 5：编译 + 联调 + commit**

运行：`cd redisui/web && npm run build`；对补齐功能逐项验证。
```bash
git add redisui/cluster.go redisui/web/src/components/contents redisui/web/src/components/Status.vue redisui/web/src/components/MemoryAnalysis.vue redisui/web/src/components/SlowLog.vue redisui/web/src/components/viewers
git commit -m "feat(redisui,web): cluster nodes, stream/rejson/timeseries/vector, monitor, import/export"
```

### 任务 11：可插拔鉴权中间件 + 打包收尾

**文件：**
- 修改：`redisui/redisui.go`、`redisui/redisui_test.go`
- 创建：`example/main.go`（示例宿主程序）、`example/README.md`
- 创建：`README.md`（库用法文档）

- [ ] **步骤 1：中间件生效测试**

```go
func TestMiddlewareWrapsAllRoutes(t *testing.T) { /* 注入要求 header 的 Middleware，无 header 请求得 401 */ }
func TestEnabledToggleShowsOrHidesConnection(t *testing.T) { /* Enabled=false 时 /api/connections 不出现该连接且直连其命令 404 */ }
```

- [ ] **步骤 2：实现中间件包裹**

在 `Register`/`server` 中，将所有已挂载路由统一经过 `cfg.Middleware`。

- [ ] **步骤 3：编写示例与用法文档**

`example/main.go` 展示 `Register(mux, redisui.Config{...})` + 起服务；`README.md` 写注册/注入/鉴权/构建说明。

- [ ] **步骤 4：运行测试 + Go 全量测试**

运行：`go test ./...` 与 `cd redisui/web && npx vitest run`
预期：全部 PASS。

- [ ] **步骤 5：Commit**

```bash
git add redisui/redisui.go redisui/redisui_test.go example README.md
git commit -m "feat(redisui): pluggable auth middleware, example app, docs"
```

### 任务 12：全链路集成验证与最终审查

**文件：**
- 创建：`redisui/integration_test.go`（用 miniredis 串起 注册→命令→scan→静态页 的端到端）

- [ ] **步骤 1：编写端到端集成测试**

```go
func TestFullEmbeddedFlow(t *testing.T) { /* 起 httptest server + 注册含 miniredis 的连接；请求首页 200、连接列表含该连接、command 可 get、scan 可翻页 */ }
```

- [ ] **步骤 2：运行全量测试**

运行：`go test ./... -race && cd redisui/web && npx vitest run`
预期：全部 PASS。

- [ ] **步骤 3：最终审查 + Commit**

对照规格再次核查；`git add -A && git commit -m "test(redisui): end-to-end embedded flow"`。
