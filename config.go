package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Config 程序配置。所有与超星有关的地址均可通过配置文件调整，
// 避免官方调整接口路径后需要重新编译。
type Config struct {
	// 账号信息
	Username string `json:"username"`
	Password string `json:"password"`

	// 登录接口（fanyalogin 会通过 Set-Cookie 写入登录态）
	LoginURL string `json:"login_url"`

	// 办公(座位)系统根地址
	BaseOffice string `json:"base_office"`

	// 座位基本信息。room_id = 链接中 id=5703；seat_id = 链接中 seatId=105；
	// seat_num = 座位号(链接中 seatNum=117，提交时自动补齐3位)。
	RoomID  string `json:"room_id"`
	SeatID  string `json:"seat_id"`
	SeatNum string `json:"seat_num"`

	// 预约时间段 (HH:MM) —— 若 day 留空则默认当天
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Day       string `json:"day"`

	// 智能时段：自动选择当下可约时段；单次时长上限与场地关闭时间
	AutoSlot              bool   `json:"auto_slot"`
	ReserveDurationMinute int    `json:"reserve_duration_minutes"` // 单次预约时长(分钟)，默认240(4h)
	CapEnd                string `json:"cap_end"`                  // 场地关闭时间 HH:MM，默认22:00
	RenewAheadMinute      int    `json:"renew_ahead_minutes"`      // 当前时段结束前多久开始预约下一时段(分钟)，默认10

	// 自动签到：等待签到窗口(开始前 sign_ahead 分钟)自动调用签到接口
	AutoSign         bool `json:"auto_sign"`
	SignAheadMinute  int  `json:"sign_ahead_minutes"`

	// 安全验证：auto 自动解滑块；captcha 可手动指定已获取的 validate token
	AutoCaptcha bool   `json:"auto_captcha"`
	Captcha     string `json:"captcha"`

	// 抢占其他座位：抢座失败后依次尝试的备用座位号列表
	AltSeats []string `json:"alt_seats"`

	// 获取 token 的页面路径及备用路径（token 从返回页面 JS 中提取）
	TokenPath     string `json:"token_path"`
	TokenPathAlt  string `json:"token_path_alt"`
	TokenPageParams string `json:"token_page_params"` // 额外 query 参数，如 day=2026-09-01&backLevel=2

	// 自动模式
	GrabAt              string `json:"grab_at"`                 // HH:MM:SS 自动开始抢座
	RenewStartTime      string `json:"renew_start_time"`        // 续约起始时间 (HH:MM)，默认 = end_time
	RenewEndTime        string `json:"renew_end_time"`          // 续约后的结束时间 (HH:MM)
	RenewIntervalMinute int    `json:"renew_interval_minutes"`  // 续约间隔(分钟)
	RequestIntervalMs   int    `json:"request_interval_ms"`     // 抢座请求间隔(毫秒)，建议 >= 500
	MaxRetry            int    `json:"max_retry"`               // 抢座最大尝试次数(0 = 无限)

	// 其他
	UserAgent  string `json:"user_agent"`
	Debug      bool   `json:"debug"`
}

// DefaultConfig 返回带默认值的配置。
func DefaultConfig() *Config {
	return &Config{
		LoginURL:     "https://passport2.chaoxing.com/fanyalogin",
		BaseOffice:   "https://office.chaoxing.com",
		TokenPath:    "/front/apps/seatengine/code",
		TokenPathAlt: "/front/third/apps/seatengine/select",
		GrabAt:       "07:00:00",
		RenewEndTime: "22:00",
		RenewIntervalMinute: 60,
		RequestIntervalMs:   800,
		MaxRetry:            0,
		UserAgent:             "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		AutoCaptcha:           true,
		AutoSlot:              true,
		ReserveDurationMinute: 240,
		CapEnd:                "22:00",
		RenewAheadMinute:      10,
		AutoSign:              true,
		SignAheadMinute:       20,
		Debug:                 false,
	}
}

// LoadConfig 读取配置文件，缺省字段补默认值。
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("配置文件不存在: %s (请复制 config.example.json 为 config.json 并填写)", path)
		}
		return nil, err
	}
	// 先解析到 map，再合并默认值，保证缺省字段有值
	raw := map[string]any{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("配置文件解析失败: %w", err)
	}
	merged, _ := json.Marshal(mergeDefaults(raw, cfg))
	if err := json.Unmarshal(merged, cfg); err != nil {
		return nil, fmt.Errorf("配置合并失败: %w", err)
	}
	return cfg, nil
}

func mergeDefaults(raw map[string]any, def *Config) map[string]any {
	b, _ := json.Marshal(def)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	for k, v := range raw {
		m[k] = v
	}
	return m
}

// Validate 校验必填字段。
func (c *Config) Validate(mode string) error {
	var missing []string
	if c.Username == "" {
		missing = append(missing, "username")
	}
	if c.Password == "" {
		missing = append(missing, "password")
	}
	if c.RoomID == "" {
		missing = append(missing, "room_id")
	}
	if c.SeatID == "" {
		missing = append(missing, "seat_id")
	}
	if mode == "book" || mode == "watch" || mode == "renew" {
		if c.StartTime == "" {
			missing = append(missing, "start_time")
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("配置缺少必填项: %s", strings.Join(missing, ", "))
	}
	return nil
}
