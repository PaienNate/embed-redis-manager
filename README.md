# embedRedisClient

把 [AnotherRedisDesktopManager](https://github.com/qishibo/AnotherRedisDesktopManager)（ARDM）改造成**可内嵌的 Go 依赖库**：宿主注入自己已初始化的 go-redis client，库在自己的 `http.ServeMux` 前缀下挂载一个同源的 Redis 管理页面，用于在"Redis 难调试、无外网地址、内网账号权限不统一"等特殊环境下做数据库调整。

## 特性

- **注入式连接**：宿主注入 `*redis.Client` / `*redis.ClusterClient`，库不建连接。
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
)

func main() {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

	mux := http.NewServeMux()
	err := redisui.Register(mux, redisui.Config{
		Prefix:  "/redisui",          // 默认 /redisui
		Version: "myapp-1.0.0",       // 设置页显示的版本号
		Connections: []redisui.Connection{
			{ID: "local", Name: "Local Redis", Client: client, Enabled: true},
		},
		// Middleware: 你的鉴权中间件（可选）
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Fatal(http.ListenAndServe(":8080", mux)) // 访问 http://<host>:8080/redisui/
}
```

## 构建与测试

```bash
# 前端（产物 dist 已提交，随前端改动需重建）
cd redisui/web && npm install && npm run build
# 图标子集（增删 fa-* 图标后）
cd redisui/web && npm run subset-icons

# Go
go build ./...
go test ./...          # 使用 miniredis，无需真实 Redis
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
