package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Scheduler 任务引擎：周期处理所有活动任务（抢座/签到/续约/跨天）。
type Scheduler struct {
	db        *gorm.DB
	mu        sync.Mutex
	clients   map[uint]*CXClient
	solver    *CaptchaSolver
	cfg       *AppConfig
	secretKey []byte
}

// NewScheduler 创建任务引擎。
func NewScheduler(db *gorm.DB, cfg *AppConfig, secretKey []byte) *Scheduler {
	return &Scheduler{db: db, clients: map[uint]*CXClient{}, solver: NewCaptchaSolver(), cfg: cfg, secretKey: secretKey}
}

// invalidateClient 使缓存客户端失效，下次任务自动重新登录。
func (s *Scheduler) invalidateClient(userID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, userID)
}

// client 获取（并登录）指定用户的超星客户端。
func (s *Scheduler) client(user *User) (*CXClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[user.ID]; ok {
		return c, nil
	}
	c := NewCXClient(s.cfg.CXBase, s.cfg.CXLoginURL, user.SeatID, "", "")
	c.SetSchool(user.DeptIDEnc, user.SeatIDEnc, user.CaptchaID)
	// 解密存储密码（AES-256-GCM）后登录
	pwd, err := decryptSecret(s.secretKey, user.Password)
	if err != nil {
		return nil, fmt.Errorf("密码解密失败(请重新登录更新存储): %v", err)
	}
	if err := c.Login(user.Username, string(pwd)); err != nil {
		return nil, err
	}
	s.clients[user.ID] = c
	return c, nil
}

// Run 主循环。
func (s *Scheduler) Run() {
	log.Println("[调度器] 启动，每20秒扫描任务")
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.tick()
	}
}

func (s *Scheduler) tick() {
	var tasks []Task
	if err := s.db.Where("status = ?", "active").Find(&tasks).Error; err != nil {
		log.Println("[调度器] 查询任务失败:", err)
		return
	}
	for i := range tasks {
		s.process(&tasks[i])
	}
}

func (s *Scheduler) process(t *Task) {
	var user User
	if err := s.db.First(&user, t.UserID).Error; err != nil {
		log.Printf("[任务%d] 用户不存在: %v", t.ID, err)
		return
	}
	c, err := s.client(&user)
	if err != nil {
		s.setTask(t, fmt.Sprintf("客户端登录失败: %v", err), false)
		return
	}
	// 会话保鲜：检测超星登录是否失效，失效则重登一次（cookie 过期自愈）
	if _, _, merr := c.MyReserves(t.SeatID); merr != nil {
		log.Printf("[任务%d] 超星会话可能失效(%v)，自动重新登录", t.ID, merr)
		s.invalidateClient(user.ID)
		if c2, err2 := s.client(&user); err2 == nil {
			c = c2
		}
	}

	// 遍历/校验闭馆时间
	if t.CapEnd == "" {
		if capEnd, err := c.RoomCapEnd(t.RoomID); err == nil {
			t.CapEnd = capEnd
			dbSave(s.db, t)
		}
	} 
	dur := time.Hour * 4
	if t.DurationMinutes > 0 {
		dur = time.Duration(t.DurationMinutes) * time.Minute
	}
	startTime := t.StartTime
	if startTime == "" {
		startTime = "08:00"
	}
	renewAhead := 10 * time.Minute

	switch t.Mode {
	case "today_once":
		s.doOneShot(c, t, 0, startTime, dur)
	case "tomorrow_once":
		s.doOneShot(c, t, 1, startTime, dur)
	case "both", "qr":
		s.doDaily(c, t, startTime, dur, renewAhead)
	default:
		s.setTask(t, "未知任务模式: "+t.Mode, false)
	}
}

// capEndFor 按目标日期遍历闭馆时间（特殊开放时间优先），并回写任务展示值。
func (s *Scheduler) capEndFor(c *CXClient, t *Task, day time.Time) string {
	if e, err := c.RoomCapEndAt(t.RoomID, day); err == nil && e != "" {
		if t.CapEnd != e {
			t.CapEnd = e
			dbSave(s.db, t)
		}
		return e
	}
	if t.CapEnd != "" {
		return t.CapEnd
	}
	return "22:00"
}

// doOneShot 一次性预约（today_once / tomorrow_once）。
func (s *Scheduler) doOneShot(c *CXClient, t *Task, dayOffset int, startTime string, dur time.Duration) {
	target := time.Now().AddDate(0, 0, dayOffset)
	day := target.Format("2006-01-02")
	cur, near, _ := c.MyReserves(t.SeatID)
	all := append(cur, near...)
	myRes := filterTaskReservesOnDay(all, t, target) // 目标日、有效状态
	if len(myRes) > 0 {
		// 已有预约：处理签到
		s.handleSign(c, t, myRes)
		return
	}

	// 预约窗口：目标日前一天 19:00 开启（预留时长固定 19:00）
	prev := target.AddDate(0, 0, -1)
	openAt := time.Date(prev.Year(), prev.Month(), prev.Day(), 19, 0, 0, 0, prev.Location())
	if time.Now().Before(openAt) {
		s.setTask(t, fmt.Sprintf("等待预约窗口开启(%s)", openAt.Format("2006-01-02 15:04")), true)
		return
	}
	capEnd := s.capEndFor(c, t, target)
	segStart, segEnd := segment(c, t, target, startTime, dur, capEnd)
	if segEnd.Sub(segStart) < time.Hour {
		s.setTask(t, "目标日剩余时段不足1小时", false)
		return
	}
	if err := s.book(c, t, day, segStart, segEnd); err != nil {
		s.setTask(t, "预约失败: "+err.Error(), false)
		return
	}
	s.setTask(t, fmt.Sprintf("已预约 %s %s~%s", day, segStart.Format("15:04"), segEnd.Format("15:04")), true)
}

// doDaily 每日循环占座（both / qr：今天到闭馆、明天到闭馆、天天循环）。
func (s *Scheduler) doDaily(c *CXClient, t *Task, startTime string, dur, renewAhead time.Duration) {
	now := time.Now()
	cur, near, _ := c.MyReserves(t.SeatID)
	all := append(cur, near...)
	myRes := filterTaskReserves(all, t)
	capEnd := s.capEndFor(c, t, now)
	capTime, err := parseHM(capEnd)
	if err != nil {
		capTime, _ = time.Parse("15:04", "22:00")
	}

	// 找当前最新一段有效预约
	var latestEnd time.Time
	for _, r := range myRes {
		if r.Status == 0 || r.Status == 1 || r.Status == 3 || r.Status == 5 || r.Status == 9 {
			e := time.UnixMilli(r.EndTime)
			if e.After(latestEnd) {
				latestEnd = e
			}
		}
	}

	// 签到
	s.handleSign(c, t, myRes)

	// 无任何预约 -> 从今天开始
	if latestEnd.IsZero() {
		day := now.Format("2006-01-02")
		segStart, segEnd := segment(c, t, now, startTime, dur, capEnd)
		if segEnd.Sub(segStart) < time.Hour {
			s.setTask(t, "今日剩余时段不足1小时", false)
			return
		}
		if err := s.book(c, t, day, segStart, segEnd); err != nil {
			s.setTask(t, "预约失败: "+err.Error(), false)
			return
		}
		s.setTask(t, fmt.Sprintf("已预约 %s %s~%s", day, segStart.Format("15:04"), segEnd.Format("15:04")), true)
		return
	}

	// 已有预约：续约下一段
	nextStart := latestEnd
	if nextStart.Before(now) {
		nextStart = ceilQuarter(now.Add(2 * time.Minute))
	}
	capTime = time.Date(nextStart.Year(), nextStart.Month(), nextStart.Day(), capTime.Hour(), capTime.Minute(), 0, 0, nextStart.Location())
	nextEnd := nextStart.Add(dur)
	if nextEnd.After(capTime) {
		nextEnd = capTime
	}
	if nextEnd.Sub(nextStart) >= time.Hour {
		// 等到结束前 renewAhead 再续约
		if time.Until(nextStart.Add(-renewAhead)) > 0 {
			s.setTask(t, fmt.Sprintf("已预约至 %s，下次续约 %s", latestEnd.Format("2006-01-02 15:04"), nextStart.Add(-renewAhead).Format("01-02 15:04")), true)
			return
		}
		day := nextStart.Format("2006-01-02")
		if err := s.book(c, t, day, nextStart, nextEnd); err != nil {
			s.setTask(t, "续约失败: "+err.Error(), false)
			return
		}
		s.setTask(t, fmt.Sprintf("续约成功 %s %s~%s", day, nextStart.Format("15:04"), nextEnd.Format("15:04")), true)
		return
	}

	// 今日已到闭馆 -> 预约明日第一段
	tomorrow := time.Now().AddDate(0, 0, 1)
	day := tomorrow.Format("2006-01-02")
	prev := tomorrow.AddDate(0, 0, -1)
	openAt := time.Date(prev.Year(), prev.Month(), prev.Day(), 19, 0, 0, 0, prev.Location())
	if time.Now().Before(openAt) {
		s.setTask(t, "今日占座至闭馆，等待明日窗口(19:00)", true)
		return
	}
	tomorrowCap := s.capEndFor(c, t, tomorrow)
	segStart, segEnd := segment(c, t, tomorrow, startTime, dur, tomorrowCap)
	if segEnd.Sub(segStart) < time.Hour {
		s.setTask(t, "明日剩余时段不足1小时", false)
		return
	}
	if err := s.book(c, t, day, segStart, segEnd); err != nil {
		s.setTask(t, "明日预约失败: "+err.Error(), false)
		return
	}
	s.setTask(t, fmt.Sprintf("已预约明日 %s~%s", segStart.Format("15:04"), segEnd.Format("15:04")), true)
}

// handleSign 处理待签到预约。
func (s *Scheduler) handleSign(c *CXClient, t *Task, myRes []ReserveInfo) {
	now := time.Now()
	for i := range myRes {
		r := &myRes[i]
		if r.Status != 0 && r.Status != 9 {
			continue
		}
		start := time.UnixMilli(r.StartTime)
		if now.Before(start.Add(-20 * time.Minute)) {
			continue // 窗口未开
		}
		if now.After(start.Add(20 * time.Minute)) {
			continue // 窗口已过
		}
		resp, err := c.SignIn(r.ID)
		if err != nil {
			s.setTask(t, "签到失败: "+err.Error(), false)
			continue
		}
		if strings.Contains(resp, `"success":true`) {
			s.setTask(t, fmt.Sprintf("自动签到成功 (预约 %d)", r.ID), true)
		}
	}
}

// book 抢座：取码页 -> 解滑块 -> 提交。
func (s *Scheduler) book(c *CXClient, t *Task, day string, segStart, segEnd time.Time) error {
	c.RoomID = t.RoomID
	c.SeatNum = t.SeatNum
	c.SeatID = t.SeatID
	cp, err := c.FetchCodePage(t.RoomID, t.SeatNum, t.SeatID)
	if err != nil {
		return err
	}
	referer := fmt.Sprintf("%s/front/apps/seatengine/code?id=%s&seatNum=%s&seatId=%s",
		s.cfg.CXBase, t.RoomID, t.SeatNum, t.SeatID)
	token, err := s.solver.Solve(referer, 11, c.CaptchaID)
	if err != nil {
		return err
	}
	resp, err := c.Reserve(day, segStart.Format("15:04"), segEnd.Format("15:04"), t.SeatNum, token, cp.SubmitEnc)
	if err != nil {
		return err
	}
	id, endAt, err := c.ParseReserve(resp)
	if err != nil {
		return fmt.Errorf("%s (%s)", err.Error(), truncate(resp, 150))
	}
	t.ReserveID = id
	t.ReserveEndAt = endAt
	dbSave(s.db, t)
	return nil
}

// segment 计算预约时间段（目标日，用指定闭馆时间封顶）。
func segment(_ *CXClient, t *Task, target time.Time, startTime string, dur time.Duration, capEnd string) (start, end time.Time) {
	hm, err := time.Parse("15:04", startTime)
	if err != nil {
		hm, _ = time.Parse("15:04", "08:00")
	}
	start = time.Date(target.Year(), target.Month(), target.Day(), hm.Hour(), hm.Minute(), 0, 0, target.Location())
	now := time.Now()
	if start.Before(now.Add(3 * time.Minute)) {
		start = ceilQuarter(now.Add(2 * time.Minute))
	}
	end = start.Add(dur)
	if capTime, err := parseHM(capEnd); err == nil {
		cap := time.Date(target.Year(), target.Month(), target.Day(), capTime.Hour(), capTime.Minute(), 0, 0, target.Location())
		if end.After(cap) {
			end = cap
		}
	}
	return start, end
}

func filterTaskReserves(all []ReserveInfo, t *Task) []ReserveInfo {
	var out []ReserveInfo
	for _, r := range all {
		if r.RoomIDStr() == t.RoomID && r.SeatNum == padSeat(t.SeatNum) {
			out = append(out, r)
		}
	}
	return out
}

// activeReserve 判断该预约是否"有效占用"（待签到/使用中/暂离/被监督）。
// status=2(退座)、7(已取消) 视为已释放，不应阻止再次预约。
func activeReserve(r ReserveInfo) bool {
	switch r.Status {
	case 0, 1, 3, 5, 9:
		return true
	default:
		return false
	}
}

// filterTaskReservesOnDay 仅保留"目标日 + 有效状态"的预约。
// 用于一次性预约：已退座/已取消的历史记录不能再当"已有预约"，否则永远无法重新预约。
func filterTaskReservesOnDay(all []ReserveInfo, t *Task, target time.Time) []ReserveInfo {
	var out []ReserveInfo
	for _, r := range filterTaskReserves(all, t) {
		if !activeReserve(r) {
			continue
		}
		st := time.UnixMilli(r.StartTime)
		if st.Year() == target.Year() && st.YearDay() == target.YearDay() {
			out = append(out, r)
		}
	}
	return out
}

func ceilQuarter(now time.Time) time.Time {
	r := ((now.Minute() + 14) / 15) * 15
	return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), r, 0, 0, now.Location())
}

func parseHM(s string) (time.Time, error) {
	return time.Parse("15:04", strings.TrimSpace(s))
}

func (s *Scheduler) setTask(t *Task, action string, ok bool) {
	t.LastAction = action
	t.LastOK = ok
	dbSave(s.db, t)
}

func dbSave(db *gorm.DB, t *Task) {
	if t.ID > 0 {
		db.Model(&Task{}).Where("id = ?", t.ID).Updates(map[string]any{
			"last_action":    t.LastAction,
			"last_ok":        t.LastOK,
			"reserve_id":     t.ReserveID,
			"reserve_end_at": t.ReserveEndAt,
			"cap_end":        t.CapEnd,
		})
	}
}

// 简化依赖的小工具
func strToInt(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
