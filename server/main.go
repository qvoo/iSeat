package main

import (
	"encoding/base64"
	"log"

	"gorm.io/gorm"
)

func main() {
	cfg := loadConfig()
	db, err := OpenDB(cfg)
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	backfillSchools(db, cfg) // 为老账号补默认学校参数
	backfillAutoRenew(db)    // 为老任务补默认持续续约
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

// backfillSchools 为缺少学校参数的老账号补默认值（多校支持迁移）。
func backfillSchools(db *gorm.DB, cfg *AppConfig) {
	var users []User
	db.Find(&users)
	changed := false
	for i := range users {
		u := &users[i]
		before := u.SchoolString()
		u.fillSchool(cfg)
		if u.SchoolString() != before {
			db.Model(&User{}).Where("id = ?", u.ID).Updates(map[string]any{
				"seat_id": u.SeatID, "dept_id_enc": u.DeptIDEnc,
				"seat_id_enc": u.SeatIDEnc, "captcha_id": u.CaptchaID,
			})
			changed = true
		}
	}
	if changed {
		log.Println("[迁移] 已为老账号回填默认学校参数")
	}
}

// backfillAutoRenew 为尚未标记持续续约的老任务补默认开启（抢到座位后持续续约+签到）。
func backfillAutoRenew(db *gorm.DB) {
	// 一次性任务(seat/quick)未显式关闭 auto_renew 的，默认开启持续续约
	res := db.Model(&Task{}).Where("auto_renew = ?", false).Updates(map[string]any{"auto_renew": true})
	if res.Error == nil && res.RowsAffected > 0 {
		log.Printf("[迁移] 已为 %d 个老任务补默认持续续约", res.RowsAffected)
	}
}

var _ = base64.StdEncoding
