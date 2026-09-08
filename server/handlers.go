package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Server 应用服务。
type Server struct {
	db        *gorm.DB
	cfg       *AppConfig
	scheduler *Scheduler
	secretKey []byte
}

func newServer(db *gorm.DB, cfg *AppConfig) *Server {
	key, _ := ensureSecretKey(secretDir(cfg))
	s := &Server{db: db, cfg: cfg, scheduler: NewScheduler(db, cfg, key), secretKey: key}
	return s
}

func secretDir(cfg *AppConfig) string {
	dir := filepath.Dir(cfg.SQLitePath)
	if dir == "" || dir == "." {
		dir = "data"
	}
	return dir
}

func (s *Server) authUser(c *gin.Context) (*User, bool) {
	token := c.GetHeader("Authorization")
	if token == "" {
		token, _ = c.Cookie("token")
	}
	var st SessionToken
	if err := s.db.Where("token = ? AND expires_at > ?", strings.TrimPrefix(token, "Bearer "), time.Now()).First(&st).Error; err != nil {
		return nil, false
	}
	var u User
	if err := s.db.First(&u, st.UserID).Error; err != nil {
		return nil, false
	}
	return &u, true
}

// POST /api/login
func (s *Server) handleLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入账号和密码"})
		return
	}
	// 用超星登录验证账号
	probe := NewCXClient(s.cfg.CXBase, s.cfg.CXLoginURL, s.cfg.CXSeatID, "", "")
	if err := probe.Login(req.Username, req.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	// 密码用 AES-256-GCM 加密存储（密钥来自环境变量/独立密钥文件，不写死在代码里）
	enc, _ := encryptSecret(s.secretKey, []byte(req.Password))
	u := User{Username: req.Username, Password: enc}
	u.fillSchool(s.cfg) // 默认学校
	var existing User
	if err := s.db.Where("username = ?", req.Username).First(&existing).Error; err == nil {
		u = existing
		u.fillSchool(s.cfg)
		s.db.Model(&u).Update("password", enc)
		s.db.Model(&u).Updates(map[string]any{
			"seat_id": u.SeatID, "dept_id_enc": u.DeptIDEnc,
			"seat_id_enc": u.SeatIDEnc, "captcha_id": u.CaptchaID,
		})
	} else {
		s.db.Create(&u)
	}
	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)
	s.db.Create(&SessionToken{Token: token, UserID: u.ID, ExpiresAt: time.Now().Add(30 * 24 * time.Hour)})
	uid := ""
	if _, _, e := probe.MyReserves(u.SeatID); e == nil {
		uid = ""
	}
	s.db.Model(&u).Update("uid", uid)
	c.JSON(http.StatusOK, gin.H{"token": token, "user": gin.H{"id": u.ID, "username": u.Username}})
}

// GET /api/rooms
func (s *Server) handleRooms(c *gin.Context) {
	user, ok := s.authUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	target := *user
	if aid, _ := strconv.Atoi(c.Query("account_id")); aid > 0 {
		var tgt User
		if err := s.db.First(&tgt, aid).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "账号不存在"})
			return
		}
		tgt.fillSchool(s.cfg)
		target = tgt
	}
	target.fillSchool(s.cfg)
	cx, err := s.scheduler.client(&target)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "登录超星失败: " + err.Error()})
		return
	}
	day := c.Query("day")
	if day == "" {
		day = time.Now().Format("2006-01-02")
	}
	rooms, err := cx.RoomList(target.SeatID, day)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rooms": rooms, "day": day, "school": gin.H{"seat_id": target.SeatID, "dept_id_enc": target.DeptIDEnc, "captcha_id": target.CaptchaID}})
}

// POST /api/tasks  创建占座任务
// req: {type: "seat"|"qr"|"quick", mode: "today_once"|"tomorrow_once"|"both"|"qr",
//       room_id, seat_id, seat_num, start_time, duration_minutes, room_name, recur_daily, qr_image(b64, 可选)}
func (s *Server) handleCreateTask(c *gin.Context) {
	user, ok := s.authUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req struct {
		Type            string `json:"type"`
		Mode            string `json:"mode"`
		RoomID          string `json:"room_id"`
		SeatID          string `json:"seat_id"`
		SeatNum         string `json:"seat_num"`
		RoomName        string `json:"room_name"`
		StartTime       string `json:"start_time"`
		DurationMinutes int    `json:"duration_minutes"`
		RecurDaily      bool   `json:"recur_daily"`
		AutoRenew       *bool  `json:"auto_renew"` // 抢座后持续续约（默认开）
		QRImage         string `json:"qr_image"`
		AccountID       uint   `json:"account_id"` // 单账号作用域
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if req.StartTime == "" {
		req.StartTime = "08:00"
	}
	if req.DurationMinutes <= 0 {
		req.DurationMinutes = 240
	}
	if req.RoomID == "" || req.SeatNum == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少座位信息"})
		return
	}
	// 账号作用域：默认当前登录账号，可按 account_id 指定其他账号
	targetUser := *user
	if req.AccountID > 0 {
		var tgt User
		if err := s.db.First(&tgt, req.AccountID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "账号不存在"})
			return
		}
		targetUser = tgt
	}
	targetUser.fillSchool(s.cfg)
	if req.SeatID == "" {
		req.SeatID = targetUser.SeatID
	}
	// 遍历该房间闭馆时间（使用该账号学校客户端）
	cx, err := s.scheduler.client(&targetUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "超星登录失败: " + err.Error()})
		return
	}
	capEnd, _ := cx.RoomCapEnd(req.RoomID)
	if capEnd == "" {
		capEnd = "22:00"
	}
	// 默认模式补充
	if req.Type == "seat" || req.Type == "quick" {
		if req.Mode == "" {
			req.Mode = "today_once"
		}
	}
	// 持续续约：seat/quick 默认开启；用户在确认弹窗可关闭
	autoRenew := true
	if req.AutoRenew != nil {
		autoRenew = *req.AutoRenew
	}
	task := Task{
		UserID:          targetUser.ID,
		Type:            req.Type,
		Mode:            req.Mode,
		RoomID:          req.RoomID,
		SeatID:          req.SeatID,
		SeatNum:         padSeat(req.SeatNum),
		RoomName:        req.RoomName,
		StartTime:       req.StartTime,
		DurationMinutes: req.DurationMinutes,
		CapEnd:          capEnd,
		RecurDaily:      req.RecurDaily,
		AutoRenew:       autoRenew,
		Status:          "active",
		LastAction:      "任务已创建，等待调度",
	}
	s.db.Create(&task)
	c.JSON(http.StatusOK, gin.H{"task": task})
}

// GET /api/tasks  列出所有账号的任务（批量/多账号系统，展示账号名）。
func (s *Server) handleTasks(c *gin.Context) {
	if _, ok := s.authUser(c); !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var tasks []Task
	s.db.Order("id desc").Find(&tasks)
	// 联表账号名
	var users []User
	s.db.Find(&users)
	uname := map[uint]string{}
	for _, u := range users {
		uname[u.ID] = u.Username
	}
	for i := range tasks {
		tasks[i].Username = uname[tasks[i].UserID]
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

// POST /api/tasks/:id/action  {action: "pause"|"resume"|"remove"}
func (s *Server) handleTaskAction(c *gin.Context) {
	if _, ok := s.authUser(c); !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	var task Task
	if err := s.db.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	_ = c.ShouldBindJSON(&req)
	switch req.Action {
	case "pause":
		task.Status = "paused"
		s.db.Save(&task)
	case "resume":
		task.Status = "active"
		s.db.Save(&task)
	case "remove":
		s.db.Delete(&task)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "未知操作"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /api/my-reserves
func (s *Server) handleMyReserves(c *gin.Context) {
	user, ok := s.authUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	// 账号作用域：?account_id= 指定其他账号
	target := *user
	if aid, _ := strconv.Atoi(c.Query("account_id")); aid > 0 {
		var tgt User
		if err := s.db.First(&tgt, aid).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "账号不存在"})
			return
		}
		target = tgt
	}
	target.fillSchool(s.cfg)
	cx, err := s.scheduler.client(&target)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "超星登录失败: " + err.Error()})
		return
	}
	cur, near, err := cx.MyReserves(target.SeatID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"cur": cur, "near": near})
}

// POST /api/reserve-action {action:"cancel"|"signback", reserve_id, account_id?}
// account_id 用于对"其他账号"的预约操作（取消/退座）；缺省用当前登录账号。
func (s *Server) handleReserveAction(c *gin.Context) {
	user, ok := s.authUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req struct {
		Action    string `json:"action"`
		ReserveID int64  `json:"reserve_id"`
		AccountID uint   `json:"account_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ReserveID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	target := *user
	if req.AccountID > 0 {
		var tgt User
		if err := s.db.First(&tgt, req.AccountID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "账号不存在"})
			return
		}
		tgt.fillSchool(s.cfg)
		target = tgt
	}
	target.fillSchool(s.cfg)
	cx, err := s.scheduler.client(&target)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "超星登录失败: " + err.Error()})
		return
	}
	var resp string
	switch req.Action {
	case "cancel":
		resp, err = cx.CancelReserve(req.ReserveID)
	case "signback":
		resp, err = cx.SignBack(req.ReserveID)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "未知操作"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if strings.Contains(resp, `"success":true`) {
		c.JSON(http.StatusOK, gin.H{"ok": true, "msg": resp})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": false, "msg": resp})
}

// GET /api/rooms/:id/seats  房间座位网格（亮=可选）。
func (s *Server) handleRoomSeats(c *gin.Context) {
	user, ok := s.authUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	roomID := c.Param("id")
	target := *user
	if aid, _ := strconv.Atoi(c.Query("account_id")); aid > 0 {
		var tgt User
		if err := s.db.First(&tgt, aid).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "账号不存在"})
			return
		}
		tgt.fillSchool(s.cfg)
		target = tgt
	}
	target.fillSchool(s.cfg)
	day := c.Query("day")
	if day == "" {
		day = time.Now().Format("2006-01-02")
	}
	targetDay, err := time.Parse("2006-01-02", day)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日期格式错误"})
		return
	}
	cx, err := s.scheduler.client(&target)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "超星登录失败: " + err.Error()})
		return
	}
	start := c.Query("start")
	if start == "" {
		start = "08:00"
	}
	end := c.Query("end")
	if end == "" {
		if capEnd, err := cx.RoomCapEndAt(roomID, targetDay); err == nil {
			end = capEnd
		} else {
			end = "22:00"
		}
	}
	seats, err := cx.RoomSeats(target.SeatID, roomID, day, start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"seats": seats, "day": day, "start": start, "end": end})
}

// GET /api/me  当前登录账号信息。
func (s *Server) handleMe(c *gin.Context) {
	user, ok := s.authUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	user.fillSchool(s.cfg)
	c.JSON(http.StatusOK, gin.H{"user": gin.H{
		"id": user.ID, "username": user.Username,
		"seat_id": user.SeatID, "dept_id_enc": user.DeptIDEnc,
		"seat_id_enc": user.SeatIDEnc, "captcha_id": user.CaptchaID, "school": user.SchoolString(),
	}})
}

// GET /api/ping
func (s *Server) handlePing(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": true, "time": time.Now().Format("2006-01-02 15:04:05")})
}

// setupRouter 组装路由。
func (s *Server) setupRouter() *gin.Engine {
	r := gin.Default()
	r.MaxMultipartMemory = 8 << 20
	r.POST("/api/login", s.handleLogin)
	r.GET("/api/ping", s.handlePing)
	api := r.Group("/api", s.authMiddleware)
	{
		api.GET("/me", s.handleMe)
		api.GET("/rooms", s.handleRooms)
		api.GET("/rooms/:id/seats", s.handleRoomSeats)
		api.POST("/tasks", s.handleCreateTask)
		api.GET("/tasks", s.handleTasks)
		api.POST("/tasks/:id/action", s.handleTaskAction)
		api.GET("/my-reserves", s.handleMyReserves)
		api.POST("/reserve-action", s.handleReserveAction)
		api.GET("/accounts", s.handleAccounts)
		api.POST("/accounts", s.handleAddAccount)
		api.POST("/accounts/batch-delete", s.handleBatchDeleteAccounts)
		api.PUT("/accounts/:id/school", s.handleAccountSchool)
		api.DELETE("/accounts/:id", s.handleDeleteAccount)
		api.POST("/batch-task", s.handleBatchTask)
	}
	r.Static("/assets", s.cfg.WebDir+"/assets")
	r.StaticFile("/", s.cfg.WebDir+"/index.html")
	r.StaticFile("/index.html", s.cfg.WebDir+"/index.html")
	r.NoRoute(func(c *gin.Context) {
		c.File(s.cfg.WebDir + "/index.html")
	})
	return r
}

func (s *Server) authMiddleware(c *gin.Context) {
	if _, ok := s.authUser(c); !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	c.Next()
}

// 小工具
func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.ReplaceAll(s, "\n", ""))
}

var _ = json.Marshal
var _ = io.Discard

// GET /api/accounts  列出所有账号（批量选座用）。
func (s *Server) handleAccounts(c *gin.Context) {
	if _, ok := s.authUser(c); !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var list []User
	s.db.Order("id asc").Find(&list)
	type acc struct {
		ID         uint   `json:"id"`
		Username   string `json:"username"`
		SeatID     string `json:"seat_id"`
		DeptIDEnc  string `json:"dept_id_enc"`
		SeatIDEnc  string `json:"seat_id_enc"`
		CaptchaID  string `json:"captcha_id"`
		School     string `json:"school"`
		CreatedAt  string `json:"created_at"`
	}
	out := make([]acc, 0, len(list))
	for _, u := range list {
		u.fillSchool(s.cfg)
		out = append(out, acc{ID: u.ID, Username: u.Username, SeatID: u.SeatID, DeptIDEnc: u.DeptIDEnc, SeatIDEnc: u.SeatIDEnc, CaptchaID: u.CaptchaID, School: u.SchoolString(), CreatedAt: u.CreatedAt.Format("2006-01-02 15:04")})
	}
	c.JSON(http.StatusOK, gin.H{"accounts": out})
}

// POST /api/accounts  添加新超星账号（校验登录后加密存储）。
func (s *Server) handleAddAccount(c *gin.Context) {
	if _, ok := s.authUser(c); !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		SeatID     string `json:"seat_id"`
		DeptIDEnc  string `json:"dept_id_enc"`
		SeatIDEnc  string `json:"seat_id_enc"`
		CaptchaID  string `json:"captcha_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入账号和密码"})
		return
	}
	probe := NewCXClient(s.cfg.CXBase, s.cfg.CXLoginURL, s.cfg.CXSeatID, "", "")
	if err := probe.Login(req.Username, req.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "账号登录超星失败: " + err.Error()})
		return
	}
	var existing User
	if err := s.db.Where("username = ?", req.Username).First(&existing).Error; err == nil {
		c.JSON(http.StatusOK, gin.H{"account": gin.H{"id": existing.ID, "username": existing.Username}, "msg": "账号已存在"})
		return
	}
	enc, _ := encryptSecret(s.secretKey, []byte(req.Password))
	u := User{Username: req.Username, Password: enc, SeatID: req.SeatID, DeptIDEnc: req.DeptIDEnc, SeatIDEnc: req.SeatIDEnc, CaptchaID: req.CaptchaID}
	u.fillSchool(s.cfg) // 未提供的学校参数用默认值补全
	s.db.Create(&u)
	c.JSON(http.StatusOK, gin.H{"account": gin.H{"id": u.ID, "username": u.Username, "school": u.SchoolString()}})
}

// PUT /api/accounts/:id/school  更新账号学校参数（座位 seatId / 单位 enc / 验证码 captchaId）。
func (s *Server) handleAccountSchool(c *gin.Context) {
	if _, ok := s.authUser(c); !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	var u User
	if err := s.db.First(&u, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "账号不存在"})
		return
	}
	var req struct {
		SeatID    string `json:"seat_id"`
		DeptIDEnc string `json:"dept_id_enc"`
		SeatIDEnc string `json:"seat_id_enc"`
		CaptchaID string `json:"captcha_id"`
	}
	_ = c.ShouldBindJSON(&req)
	updates := map[string]any{}
	if req.SeatID != "" {
		updates["seat_id"] = req.SeatID
	}
	if req.DeptIDEnc != "" {
		updates["dept_id_enc"] = req.DeptIDEnc
	}
	if req.SeatIDEnc != "" {
		updates["seat_id_enc"] = req.SeatIDEnc
	}
	if req.CaptchaID != "" {
		updates["captcha_id"] = req.CaptchaID
	}
	if len(updates) > 0 {
		s.db.Model(&User{}).Where("id = ?", uint(id)).Updates(updates)
	}
	// 学校变更后缓存客户端失效，下次重新登录
	s.scheduler.invalidateClient(uint(id))
	var nu User
	s.db.First(&nu, id)
	nu.fillSchool(s.cfg)
	c.JSON(http.StatusOK, gin.H{"ok": true, "account": gin.H{
		"id": nu.ID, "username": nu.Username,
		"seat_id": nu.SeatID, "dept_id_enc": nu.DeptIDEnc,
		"seat_id_enc": nu.SeatIDEnc, "captcha_id": nu.CaptchaID, "school": nu.SchoolString(),
	}})
}

// DELETE /api/accounts/:id  删除账号及其任务。
// 允许删除任意账号（含当前登录账号）；删除后该账号的会话令牌一并失效。
func (s *Server) handleDeleteAccount(c *gin.Context) {
	user, ok := s.authUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效账号"})
		return
	}
	s.db.Where("user_id = ?", id).Delete(&Task{})
	s.db.Where("user_id = ?", id).Delete(&SessionToken{})
	s.db.Delete(&User{}, id)
	s.scheduler.invalidateClient(uint(id))
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted_self": uint(id) == user.ID})
}

// POST /api/accounts/batch-delete  批量删除账号 {ids:[...]}。
// 与单个删除一致：级联清理任务+会话，失效客户端；若含当前登录账号则标记 deleted_self。
func (s *Server) handleBatchDeleteAccounts(c *gin.Context) {
	user, ok := s.authUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req struct {
		IDs []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要删除的账号"})
		return
	}
	deletedSelf := false
	for _, id := range req.IDs {
		if id == 0 {
			continue
		}
		s.db.Where("user_id = ?", id).Delete(&Task{})
		s.db.Where("user_id = ?", id).Delete(&SessionToken{})
		s.db.Delete(&User{}, id)
		s.scheduler.invalidateClient(id)
		if id == user.ID {
			deletedSelf = true
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted_self": deletedSelf, "deleted": len(req.IDs)})
}

// POST /api/batch-task  批量占座：为多个账号创建任务。
// seats 为空则自动分配该房间的可用座位（每个账号一个，互不重复）。
func (s *Server) handleBatchTask(c *gin.Context) {
	admin, ok := s.authUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req struct {
		RoomID     string   `json:"room_id"`
		SeatID     string   `json:"seat_id"`
		Mode       string   `json:"mode"`
		StartTime  string   `json:"start_time"`
		RoomName   string   `json:"room_name"`
		Seats      []string `json:"seats"`
		AccountIDs []uint   `json:"account_ids"`
		AutoRenew  *bool    `json:"auto_renew"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.RoomID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if req.StartTime == "" {
		req.StartTime = "08:00"
	}
	if req.Mode == "" {
		req.Mode = "tomorrow_once"
	}
	var accounts []User
	if len(req.AccountIDs) > 0 {
		s.db.Where("id IN ?", req.AccountIDs).Find(&accounts)
	} else {
		s.db.Find(&accounts)
	}
	if len(accounts) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "没有可用的账号"})
		return
	}
	if req.SeatID == "" {
		req.SeatID = s.cfg.CXSeatID
	}
	// 多校支持：房间属于一个学校(req.SeatID)，批量只包含同校账号，跨校账号请切该校房间单独批量。
	var sameSchool []User
	for i := range accounts {
		accounts[i].fillSchool(s.cfg)
		if accounts[i].SeatID == req.SeatID {
			sameSchool = append(sameSchool, accounts[i])
		}
	}
	if len(sameSchool) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "所选账号与当前房间不属于同一学校，请切换该校房间后再批量"})
		return
	}
	accounts = sameSchool

	seats := req.Seats
	cx, _ := s.scheduler.client(admin)
	if len(seats) < len(accounts) {
		if cx != nil {
			day := time.Now().Format("2006-01-02")
			if req.Mode == "tomorrow_once" {
				day = time.Now().AddDate(0, 0, 1).Format("2006-01-02")
			}
			cells, _ := cx.RoomSeats(req.SeatID, req.RoomID, day, "08:00", "23:30")
			used := map[string]bool{}
			for _, s0 := range seats {
				used[s0] = true
			}
			for _, cell := range cells {
				if cell.Available && !used[cell.Num] {
					seats = append(seats, cell.Num)
					used[cell.Num] = true
				}
				if len(seats) >= len(accounts) {
					break
				}
			}
		}
	}
	if len(seats) < len(accounts) {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("可用座位不足: 需 %d 个账号", len(accounts))})
		return
	}

	capEnd, _ := NewCXClient(s.cfg.CXBase, s.cfg.CXLoginURL, req.SeatID, req.RoomID, "").RoomCapEnd(req.RoomID)
	if capEnd == "" {
		capEnd = "22:00"
	}
	autoRenew := true
	if req.AutoRenew != nil {
		autoRenew = *req.AutoRenew
	}
	var created []Task
	for i, acc := range accounts {
		t := Task{
			UserID: acc.ID, Type: "seat", Mode: req.Mode,
			RoomID: req.RoomID, SeatID: req.SeatID, SeatNum: padSeat(seats[i]),
			RoomName: req.RoomName, StartTime: req.StartTime, DurationMinutes: 240, CapEnd: capEnd,
			AutoRenew: autoRenew,
			Status: "active", LastAction: "批量任务已创建，等待调度",
		}
		s.db.Create(&t)
		created = append(created, t)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "created": created})
}
