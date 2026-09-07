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
	var existing User
	if err := s.db.Where("username = ?", req.Username).First(&existing).Error; err == nil {
		u = existing
		s.db.Model(&u).Update("password", enc)
	} else {
		s.db.Create(&u)
	}
	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)
	s.db.Create(&SessionToken{Token: token, UserID: u.ID, ExpiresAt: time.Now().Add(30 * 24 * time.Hour)})
	uid := ""
	if _, _, e := probe.MyReserves(s.cfg.CXSeatID); e == nil {
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
	cx, err := s.scheduler.client(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "登录超星失败: " + err.Error()})
		return
	}
	day := c.Query("day")
	if day == "" {
		day = time.Now().Format("2006-01-02")
	}
	rooms, err := cx.RoomList(s.cfg.CXSeatID, day)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rooms": rooms, "day": day})
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
		QRImage         string `json:"qr_image"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if req.SeatID == "" {
		req.SeatID = s.cfg.CXSeatID
	}
	if req.StartTime == "" {
		req.StartTime = "08:00"
	}
	if req.DurationMinutes <= 0 {
		req.DurationMinutes = 240
	}
	if req.Type == "qr" && req.QRImage != "" {
		info, err := decodeQRBase64(req.QRImage)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		req.RoomID = info.RoomID
		req.SeatNum = info.SeatNum
		req.SeatID = info.SeatID
		if req.Mode == "" {
			req.Mode = "qr"
		}
	}
	if req.RoomID == "" || req.SeatNum == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少座位信息"})
		return
	}
	// 遍历该房间闭馆时间
	cx, err := s.scheduler.client(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "超星登录失败: " + err.Error()})
		return
	}
	capEnd, _ := cx.RoomCapEnd(req.RoomID)
	if capEnd == "" {
		capEnd = "22:00"
	}
	// 二维码上传 -> 存入共享二维码库（去重）
	if req.Type == "qr" && req.RoomID != "" && req.SeatNum != "" && req.QRImage != "" {
		s.saveQrToLibrary(user.ID, req.RoomID, req.SeatID, req.SeatNum, req.RoomName, capEnd, true)
	}
	// 默认模式补充
	if req.Type == "seat" || req.Type == "quick" {
		if req.Mode == "" {
			req.Mode = "today_once"
		}
	}
	task := Task{
		UserID:          user.ID,
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
		Status:          "active",
		LastAction:      "任务已创建，等待调度",
	}
	s.db.Create(&task)
	c.JSON(http.StatusOK, gin.H{"task": task})
}

// GET /api/tasks
func (s *Server) handleTasks(c *gin.Context) {
	user, ok := s.authUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var tasks []Task
	s.db.Where("user_id = ?", user.ID).Order("id desc").Find(&tasks)
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

// POST /api/tasks/:id/action  {action: "pause"|"resume"|"remove"}
func (s *Server) handleTaskAction(c *gin.Context) {
	user, ok := s.authUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	var task Task
	if err := s.db.Where("id = ? AND user_id = ?", id, user.ID).First(&task).Error; err != nil {
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
	cx, err := s.scheduler.client(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "超星登录失败: " + err.Error()})
		return
	}
	cur, near, err := cx.MyReserves(s.cfg.CXSeatID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"cur": cur, "near": near})
}

// POST /api/reserve-action {action:"cancel"|"signback", reserve_id}
func (s *Server) handleReserveAction(c *gin.Context) {
	user, ok := s.authUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req struct {
		Action    string `json:"action"`
		ReserveID int64  `json:"reserve_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ReserveID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	cx, err := s.scheduler.client(user)
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

// decodeQRBase64 base64 图片解码二维码。
func decodeQRBase64(b64 string) (*QRInfo, error) {
	if strings.Contains(b64, "base64,") {
		b64 = b64[strings.Index(b64, "base64,")+7:]
	}
	data, err := base64Decode(b64)
	if err != nil {
		return nil, fmt.Errorf("图片数据无效")
	}
	return DecodeQR(data)
}

// saveQrToLibrary 保存/更新共享二维码库。
func (s *Server) saveQrToLibrary(userID uint, roomID, seatID, seatNum, roomName, capEnd string, bump bool) {
	var q QrCode
	if err := s.db.Where("room_id = ? AND seat_num = ?", roomID, seatNum).First(&q).Error; err != nil {
		q = QrCode{RoomID: roomID, SeatID: seatID, SeatNum: seatNum, SourceUser: userID, UploadCount: 1}
	} else if bump {
		q.UploadCount++
	}
	if q.RoomName == "" {
		q.RoomName = roomName
	}
	if capEnd != "" {
		q.CapEnd = capEnd
	}
	s.db.Save(&q)
}

// GET /api/qr-codes  共享二维码库。
func (s *Server) handleQrCodes(c *gin.Context) {
	if _, ok := s.authUser(c); !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var list []QrCode
	s.db.Order("updated_at desc").Find(&list)
	c.JSON(http.StatusOK, gin.H{"qr_codes": list})
}

// GET /api/rooms/:id/seats  房间座位网格（亮=可选）。
func (s *Server) handleRoomSeats(c *gin.Context) {
	user, ok := s.authUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	roomID := c.Param("id")
	day := c.Query("day")
	if day == "" {
		day = time.Now().Format("2006-01-02")
	}
	target, err := time.Parse("2006-01-02", day)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日期格式错误"})
		return
	}
	cx, err := s.scheduler.client(user)
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
		if capEnd, err := cx.RoomCapEndAt(roomID, target); err == nil {
			end = capEnd
		} else {
			end = "22:00"
		}
	}
	seats, err := cx.RoomSeats(s.cfg.CXSeatID, roomID, day, start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"seats": seats, "day": day, "start": start, "end": end})
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
		api.GET("/rooms", s.handleRooms)
		api.GET("/rooms/:id/seats", s.handleRoomSeats)
		api.POST("/tasks", s.handleCreateTask)
		api.GET("/tasks", s.handleTasks)
		api.POST("/tasks/:id/action", s.handleTaskAction)
		api.GET("/my-reserves", s.handleMyReserves)
		api.POST("/reserve-action", s.handleReserveAction)
		api.GET("/qr-codes", s.handleQrCodes)
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
