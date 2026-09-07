package main

import (
	"os"
)

// AppConfig 系统配置（环境变量注入）。
type AppConfig struct {
	Port       string // PORT 默认 8080
	MySQLDSN   string // MYSQL_DSN 如 seat:pass@tcp(127.0.0.1:3306)/seatbook?charset=utf8mb4&parseTime=true; 为空则使用本地 SQLite
	SQLitePath string // SQLITE_PATH 默认 seatbook.db
	CXLoginURL string
	CXBase     string
	CXSeatID   string // 座位业务 seatId(105)
	WebDir     string // 前端静态目录
	// 保留业务常量
}

func loadConfig() *AppConfig {
	return &AppConfig{
		Port:       envOr("PORT", "5251"),
		MySQLDSN:   os.Getenv("MYSQL_DSN"),
		SQLitePath: envOr("SQLITE_PATH", "seatbook.db"),
		CXLoginURL: envOr("CX_LOGIN_URL", "https://passport2.chaoxing.com/fanyalogin"),
		CXBase:     envOr("CX_BASE", "https://office.chaoxing.com"),
		CXSeatID:   envOr("CX_SEAT_ID", "105"),
		WebDir:     envOr("WEB_DIR", "../web/dist"),
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
