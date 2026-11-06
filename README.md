# embedRedisClient

把 [AnotherRedisDesktopManager](https://github.com/qishibo/AnotherRedisDesktopManager)（ARDM）改造成**可内嵌的 Go 依赖库**：宿主注入自己已初始化的 go-redis client，库在自己的 `http.ServeMux` 前缀下挂载一个同源的 Redis 管理页面，用于在"Redis 难调试、无外网地址、内网账号权限不统一"等特殊环境下做数据库调整。

## 特性

- **注入式连接**：宿主注入已初始化的 go-redis client（v9 用 `goredisv9.New`、v8 用 `goredisv8.New`），库不建连接。
- **双版本适配**：核心 `redisui` 不直接依赖 go-redis；v8 / v9 适配器可共存，库本体要求 **Go 1.23+**。
- **连接配置**：Redis 信息页展示注入 client 的 `Options`（地址、DB、连接池、超时、TLS、协议等）；密码只显示“是否设置”，TLS 证书只显示摘要。
- **同源 SPA**：前端构建产物用 `go:embed` 打进二进制，挂到 `{prefix}/`，API 在 `{prefix}/api`。
- **功能齐全**：key 浏览/搜索、string/hash/list/set/zset 查看编辑、CLI、stream/rejson/TS/vector、集群节点、监控、导入导出。
- **流式**：MONITOR / pub-sub 走 SockJS（自动 WebSocket→XHR-streaming→XHR-polling 降级），适配受限网络。
- **可插拔鉴权**：`Config.Middleware` 包裹全部路由。
- **兼容性**：现代浏览器 + **IE11**（`@vitejs/plugin-legacy` 产出 ES5 legacy bundle）。

## 快速开始

```go
package main

import (
	"log"
	"net/http"

	"github.com/redis/go-redis/v9"
	"embedRedisClient/redisui"
	"embedRedisClient/redisui/goredisv9"
)

func main() {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

	mux := http.NewServeMux()
	err := redisui.Register(mux, redisui.Config{
		Prefix:  "/redisui",          // 默认 /redisui
		Version: "myapp-1.0.0",       // 设置页显示的版本号
		Connections: []redisui.Connection{
			{ID: "local", Name: "Local Redis", Client: goredisv9.New(client), Enabled: true},
		},
		// Middleware: 你的鉴权中间件（可选）
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Fatal(http.ListenAndServe(":8080", mux)) // 访问 http://<host>:8080/redisui/
}
```

## Go 版本与 go-redis 适配

- 库本体要求 **Go 1.23+**；核心 `redisui` 不再直接依赖 go-redis，版本差异全部收敛在适配器里。
- **v9 适配器**（`redisui/goredisv9`）：依赖 `github.com/redis/go-redis/v9 v9.18.x`。注意 v9.19+ 要求 Go 1.24，v9.23 要求 Go 1.26；宿主如果自行升级 go-redis，MVS 会选高版本，Go 下限也随之提高。
- **v8 适配器**（`redisui/goredisv8`）：依赖 `github.com/go-redis/redis/v8 v8.11.5`。go-redis v8 没有现成的 Monitor helper，adapter 用原生 RESP2 连接实现 MONITOR（仅单节点）；命令、订阅、集群节点、多 db 均正常。
- v8 和 v9 是不同 import path，**同一个进程可以同时注册两种连接**。

```go
import (
	v8 "github.com/go-redis/redis/v8"
	"embedRedisClient/redisui/goredisv8"
)

c := v8.NewClient(&v8.Options{Addr: "localhost:6379"})
// Connections: []redisui.Connection{{ID: "v8", Client: goredisv8.New(c), Enabled: true}}
```

## 构建与测试

```bash
# 前端（产物 dist 已提交，随前端改动需重建）
cd redisui/web && npm install && npm run build
# 图标子集（增删 fa-* 图标后）
cd redisui/web && npm run subset-icons

# Go（Go 1.23+；go.mod 已钉 go-redis v9.18.0 + v8.11.5）
go build ./...
go test ./...          # 使用 miniredis，无需真实 Redis；含 v8/v9 adapter 测试
gofmt -l redisui/ example/
cd redisui/web && npx vitest run
```

## 文档

- 设计规格：`docs/superpowers/specs/2026-11-05-embed-redis-client-design.md`
- 实现计划：`docs/superpowers/plans/2026-11-05-embed-redis-client.md`
- 已知问题 / 限制：`KNOWN_ISSUES.md`
- 详细英文用法：`docs/README.en.md`
- 原始需求：`docs/requirements.md`

## 致谢

界面基于 [Another Redis Desktop Manager](https://github.com/qishibo/AnotherRedisDesktopManager)（MIT）。

## License

MIT（沿用上游 ARDM 的许可）。
