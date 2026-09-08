package main

import (
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// User 登录用户（chaoxing 账号）。
type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"uniqueIndex;size:64" json:"username"`
	Password  string    `json:"-"` // AES 加密后的密文存储
	UID       string    `json:"uid"`
	SeatID    string    `gorm:"size:32" json:"seat_id"`      // 该账号所属学校/单位的座位业务 seatId
	DeptIDEnc string    `gorm:"size:64" json:"dept_id_enc"`  // 该账号学校/单位 deptIdEnc
	SeatIDEnc string    `gorm:"size:64" json:"seat_id_enc"`  // 该账号学校/单位 seatIdEnc
	CaptchaID string    `gorm:"size:64" json:"captcha_id"`   // 该账号学校/单位的滑块验证码 captchaId
	CreatedAt time.Time `json:"created_at"`
}

// SessionToken 登录会话。
type SessionToken struct {
	Token     string    `gorm:"primaryKey;size:64" json:"-"`
	UserID    uint      `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Task 占座任务。
// Type: seat(手动选座) qr(扫码) quick(快速预约)
// Mode: today_once | tomorrow_once | both(今日+明日, 之后每日自动) | qr_chain(扫码, 占座到闭馆+每日)
type Task struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	UserID            uint      `gorm:"index" json:"user_id"`
	Type              string    `gorm:"size:16" json:"type"`
	Mode              string    `gorm:"size:24" json:"mode"`
	RoomID            string    `gorm:"size:32" json:"room_id"`
	SeatID            string    `gorm:"size:32" json:"seat_id"` // 座位业务ID(如105)
	SeatNum           string    `gorm:"size:16" json:"seat_num"`
	RoomName          string    `gorm:"size:128" json:"room_name"`
	StartTime         string    `gorm:"size:8" json:"start_time"` // 期望开始 HH:MM
	DurationMinutes   int       `json:"duration_minutes"`         // 单段时长
	CapEnd            string    `gorm:"size:8" json:"cap_end"`    // 该房间闭馆时间(自动遍历)
	RecurDaily        bool      `json:"recur_daily"`              // 每日重复(占座到闭馆循环)
	Status            string    `gorm:"size:16;index" json:"status"` // active|paused|done|error
	LastAction        string    `gorm:"type:text" json:"last_action"`
	LastOK            bool      `json:"last_ok"`
	ReserveID         int64     `json:"reserve_id"`         // 最近一次预约 id
	ReserveEndAt      int64     `json:"reserve_end_at"`     // 最近预约结束毫秒时间戳
	Username          string    `gorm:"-" json:"username"`  // 所属账号（联表展示）
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// QrCode 共享二维码库（用户上传的座位二维码，全员可复用）。
type QrCode struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	RoomID      string    `gorm:"size:32;index:idx_room_seat,unique" json:"room_id"`
	SeatID      string    `gorm:"size:32" json:"seat_id"`
	SeatNum     string    `gorm:"size:16;index:idx_room_seat,unique" json:"seat_num"`
	RoomName    string    `gorm:"size:128" json:"room_name"`
	CapEnd      string    `gorm:"size:8" json:"cap_end"`
	SourceUser  uint      `json:"source_user"`
	UploadCount int       `json:"upload_count"` // 被使用/上传次数
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// fillSchool 若账号缺少学校参数，回填系统默认值（保留已配置的字段）。
func (u *User) fillSchool(cfg *AppConfig) {
	if u.SeatID == "" {
		u.SeatID = cfg.CXSeatID
	}
	if u.DeptIDEnc == "" {
		u.DeptIDEnc = cfg.CXDeptIDEnc
	}
	if u.SeatIDEnc == "" {
		u.SeatIDEnc = cfg.CXSeatIDEnc
	}
	if u.CaptchaID == "" {
		u.CaptchaID = cfg.CXCptchaID
	}
}

// SchoolString 账号学校标识（用于区分不同学校）。
func (u *User) SchoolString() string {
	return u.SeatID + "/" + u.DeptIDEnc
}

// OpenDB 打开数据库（MySQL 优先，未配置则 SQLite 本地兜底）。
func OpenDB(cfg *AppConfig) (*gorm.DB, error) {
	var dial gorm.Dialector
	if cfg.MySQLDSN != "" {
		dial = mysql.Open(cfg.MySQLDSN)
	} else {
		dial = sqlite.Open(cfg.SQLitePath)
	}
	db, err := gorm.Open(dial, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&User{}, &SessionToken{}, &Task{}, &QrCode{}); err != nil {
		return nil, err
	}
	return db, nil
}
