package redisui

import "embed"

// distFS holds the built frontend (redisui/web/dist) produced by the Vite build
// in task 3. Embedding it into the Go binary lets the UI be served without any
// external files on disk. The embed pattern is relative to this file's
// directory, hence "web/dist" rather than "redisui/web/dist".
//
//go:embed all:web/dist
var distFS embed.FS
