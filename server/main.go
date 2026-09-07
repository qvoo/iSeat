package main

import (
	"encoding/base64"
	"log"
)

func main() {
	cfg := loadConfig()
	db, err := OpenDB(cfg)
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	log.Printf("数据库就绪 (db=%s)", dbName(cfg))
	srv := newServer(db, cfg)
	go srv.scheduler.Run()
	log.Printf("Gin 服务启动: http://localhost:%s", cfg.Port)
	if err := srv.setupRouter().Run(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}

func dbName(cfg *AppConfig) string {
	if cfg.MySQLDSN != "" {
		return "mysql"
	}
	return "sqlite(本地兜底, 部署配置 MYSQL_DSN 即用 MySQL)"
}

var _ = base64.StdEncoding
