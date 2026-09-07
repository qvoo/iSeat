package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Session 维护一次登录态（Cookie Jar）。
type Session struct {
	cfg    *Config
	client *http.Client
}

// CodePage 从座位页解析出的关键字段。
type CodePage struct {
	RoomID    string
	SeatNum   string
	SeatID    string
	SubmitEnc string
	ServerNow string
	RawHTML   string
}

// NewSession 创建带 Cookie 存储的会话客户端。
func NewSession(cfg *Config) (*Session, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("重定向次数过多")
			}
			return nil
		},
	}
	return &Session{cfg: cfg, client: client}, nil
}

func (s *Session) applyHeaders(req *http.Request) {
	req.Header.Set("User-Agent", s.cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
}

func (s *Session) do(req *http.Request) (int, []byte, error) {
	s.applyHeaders(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// postForm 以表单方式 POST（模拟 $.post），返回状态码+响应体。
func (s *Session) postForm(path string, form url.Values) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, s.cfg.BaseOffice+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	s.applyHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", s.cfg.BaseOffice+"/front/apps/seatengine/code?id="+
		url.QueryEscape(s.cfg.RoomID)+"&seatNum="+url.QueryEscape(s.cfg.SeatNum)+"&seatId="+url.QueryEscape(s.cfg.SeatID))
	return s.doRaw(req)
}

func (s *Session) doRaw(req *http.Request) (int, []byte, error) {
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// Login 调用 /fanyalogin 登录。成功后 Cookie Jar 中已保存登录态。
func (s *Session) Login() (string, error) {
	uname, err := AesCbcPkcs7Base64(s.cfg.Username)
	if err != nil {
		return "", fmt.Errorf("账号加密失败: %w", err)
	}
	pwd, err := AesCbcPkcs7Base64(s.cfg.Password)
	if err != nil {
		return "", fmt.Errorf("密码加密失败: %w", err)
	}

	refer := s.cfg.BaseOffice + "/front/apps/seatengine/index?seatId=" + s.cfg.SeatID
	form := url.Values{}
	form.Set("fid", "-1")
	form.Set("uname", uname)
	form.Set("password", pwd)
	form.Set("refer", refer)
	form.Set("t", "true")
	form.Set("forbidotherlogin", "0")
	form.Set("validate", "")
	form.Set("doubleFactorLogin", "0")
	form.Set("independentId", "0")

	req, err := http.NewRequest(http.MethodPost, s.cfg.LoginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	s.applyHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://passport2.chaoxing.com")

	status, body, err := s.doRaw(req)
	if err != nil {
		return "", fmt.Errorf("登录请求失败: %w", err)
	}
	text := string(body)
	if s.cfg.Debug {
		logf("登录响应 [%d]: %s", status, text)
	}

	var m map[string]any
	jsonOK := json.Unmarshal(body, &m) == nil
	success := false
	if jsonOK {
		success = jsonFieldTrue(m, "result") || jsonFieldTrue(m, "status") || jsonFieldTrue(m, "success")
		uid := fieldStr(m, "uid")
		if uid == "" {
			uid = fieldStr(m, "uid_ec")
		}
		if success {
			logf("登录成功 uid=%s", uid)
			if s.cfg.Debug {
				logf("会话 Cookie: %s", s.DescribeCookies())
			}
			return uid, nil
		}
	}
	if !jsonOK && strings.Contains(text, `"result":1`) {
		success = true
	}
	if success {
		logf("登录成功")
		return extractJSONField(text, "uid"), nil
	}

	msg := ""
	if jsonOK {
		msg = fieldStr(m, "msg")
		if msg == "" {
			msg = fieldStr(m, "message")
		}
	}
	if msg == "" {
		msg = extractJSONField(text, "msg")
	}
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d: %s", status, truncate(text, 300))
	}
	return "", fmt.Errorf("登录失败: %s", msg)
}

var (
	reRoomID    = regexp.MustCompile(`(?i)var\s+roomId\s*=\s*['"']([0-9]+)['"']`)
	reSeatNum   = regexp.MustCompile(`(?i)var\s+seatNum\s*=\s*['"'](\d+)['"']`)
	reSeatID    = regexp.MustCompile(`(?i)var\s+seatId\s*=\s*['"'](\d+)['"']`)
	reServerNow = regexp.MustCompile(`(?i)serverNow\s*=\s*(new Date\s*\(\s*'([^']+)'\s*\))`)
	reSubmitEnc = regexp.MustCompile(`(?i)id="submit_enc"\s+value="([^"]+)"`)
)

// FetchCodePage 获取座位页（code 页面），解析 roomId/seatNum/seatId/submit_enc。
func (s *Session) FetchCodePage() (*CodePage, error) {
	u := s.cfg.BaseOffice + "/front/apps/seatengine/code?id=" +
		url.QueryEscape(s.cfg.RoomID) + "&seatNum=" + url.QueryEscape(s.cfg.SeatNum) +
		"&seatId=" + url.QueryEscape(s.cfg.SeatID)

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	s.applyHeaders(req)
	req.Header.Set("Referer", "https://passport2.chaoxing.com/")
	status, body, err := s.do(req)
	if err != nil {
		return nil, fmt.Errorf("座位页请求失败: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("座位页 HTTP %d", status)
	}
	text := string(body)
	if s.cfg.Debug {
		logf("座位页大小=%d", len(text))
		f := "debug_code_page.html"
		_ = os.WriteFile(f, body, 0o644)
	}

	cp := &CodePage{RawHTML: text}
	if m := reRoomID.FindStringSubmatch(text); len(m) == 2 {
		cp.RoomID = m[1]
	}
	if m := reSeatNum.FindStringSubmatch(text); len(m) == 2 {
		cp.SeatNum = m[1]
	}
	if m := reSeatID.FindStringSubmatch(text); len(m) == 2 {
		cp.SeatID = m[1]
	}
	if m := reSubmitEnc.FindStringSubmatch(text); len(m) == 2 {
		cp.SubmitEnc = m[1]
	}
	if cp.RoomID == "" {
		cp.RoomID = s.cfg.RoomID
	}
	if cp.SeatNum == "" {
		cp.SeatNum = s.cfg.SeatNum
	}
	if cp.SeatID == "" {
		cp.SeatID = s.cfg.SeatID
	}
	if cp.SubmitEnc == "" {
		return nil, fmt.Errorf("页面未找到 submit_enc (页面片段: %s)", truncate(text, 200))
	}
	logf("座位页解析: roomId=%s seatNum=%s seatId=%s submit_enc=%s", cp.RoomID, cp.SeatNum, cp.SeatID, cp.SubmitEnc)
	return cp, nil
}

// RoomInfo 获取房间详情（seatConfig/dynamicTimes/screenTimes 等）。
func (s *Session) RoomInfo(roomID string) ([]byte, error) {
	form := url.Values{}
	form.Set("id", roomID)
	status, body, err := s.postForm("/data/apps/seatengine/room/info", form)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("room/info HTTP %d: %s", status, truncate(string(body), 200))
	}
	if s.cfg.Debug {
		logf("room/info 响应: %s", truncate(string(body), 500))
	}
	return body, nil
}

// SubmitReserve 新版提交预约：POST data/apps/seatengine/submit。
// 参数顺序必须与页面 JS paramObj 字面量顺序一致：
// roomId, day, startTime, endTime, seatNum, captcha, type, verifyData, wyToken
// submitEnc 为座位页 submit_enc 的 value。
func (s *Session) SubmitReserve(day, startTime, endTime, seatNum, captcha, submitEnc string) (string, error) {
	params := [][2]string{
		{"roomId", s.cfg.RoomID},
		{"day", day},
		{"startTime", startTime},
		{"endTime", endTime},
		{"seatNum", padSeatNum(seatNum)},
		{"captcha", captcha},
		{"type", "1"},
		{"verifyData", "1"},
		{"wyToken", ""},
	}
	enc := BuildEncNew(params, submitEnc)

	form := url.Values{}
	for _, p := range params {
		form.Set(p[0], p[1])
	}
	form.Set("enc", enc)

	if s.cfg.Debug {
		logf("submit 表单: %s", form.Encode())
	}
	status, body, err := s.postForm("/data/apps/seatengine/submit", form)
	if err != nil {
		return "", err
	}
	respText := string(body)
	if status != http.StatusOK {
		return respText, fmt.Errorf("HTTP %d", status)
	}
	if s.cfg.Debug {
		logf("submit 响应: %s", truncate(respText, 500))
	}
	return respText, nil
}

// SubmitReserveEx 提交预约的变体，用于测试不同参数组合。
// variant: 0=type+verifyData(默认), 1=wfwEngineEnc 风格(无type/verifyData), 2=verifyData=0
func (s *Session) SubmitReserveEx(day, startTime, endTime, seatNum, captcha, submitEnc string, variant int) (string, error) {
	var params [][2]string
	var form url.Values
	switch variant {
	case 1:
		params = [][2]string{
			{"roomId", s.cfg.RoomID},
			{"day", day},
			{"startTime", startTime},
			{"endTime", endTime},
			{"seatNum", padSeatNum(seatNum)},
			{"captcha", captcha},
			{"wyToken", ""},
			{"wfwEngineEnc", ""},
		}
	case 2:
		params = [][2]string{
			{"roomId", s.cfg.RoomID},
			{"day", day},
			{"startTime", startTime},
			{"endTime", endTime},
			{"seatNum", padSeatNum(seatNum)},
			{"captcha", captcha},
			{"type", "1"},
			{"verifyData", "0"},
			{"wyToken", ""},
		}
	default:
		params = [][2]string{
			{"roomId", s.cfg.RoomID},
			{"day", day},
			{"startTime", startTime},
			{"endTime", endTime},
			{"seatNum", padSeatNum(seatNum)},
			{"captcha", captcha},
			{"type", "1"},
			{"verifyData", "1"},
			{"wyToken", ""},
		}
	}
	enc := BuildEncNew(params, submitEnc)

	form = url.Values{}
	for _, p := range params {
		form.Set(p[0], p[1])
	}
	form.Set("enc", enc)

	if s.cfg.Debug {
		logf("submitx 表单(variant=%d): %s", variant, form.Encode())
	}
	status, body, err := s.postForm("/data/apps/seatengine/submit", form)
	if err != nil {
		return "", err
	}
	respText := string(body)
	if status != http.StatusOK {
		return respText, fmt.Errorf("HTTP %d", status)
	}
	if s.cfg.Debug {
		logf("submitx 响应: %s", truncate(respText, 400))
	}
	return respText, nil
}

// ReserveInfo 预约信息（来自 data/apps/seatengine/index）。
type ReserveInfo struct {
	ID             int64  `json:"id"`
	Status         int    `json:"status"`
	StartTime      int64  `json:"startTime"`
	EndTime        int64  `json:"endTime"`
	SeatNum        string `json:"seatNum"`
	SignDuration   int    `json:"signDuration"`
	PreSignMinutes int    `json:"preSignDuration"`
}

// ListCurReserves 解析当前预约列表（仅 curReserves）。
func (s *Session) ListCurReserves() ([]ReserveInfo, error) {
	raw, err := s.MyReserves()
	if err != nil {
		return nil, err
	}
	var m struct {
		Data struct {
			CurReserves []ReserveInfo `json:"curReserves"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	return m.Data.CurReserves, nil
}

// SignIn 签到：POST data/apps/seatengine/sign {id: reserveId, seatId, roomId}。
func (s *Session) SignIn(reserveID int64) (string, error) {
	form := url.Values{}
	form.Set("id", strconv.FormatInt(reserveID, 10))
	form.Set("seatId", s.cfg.SeatID)
	form.Set("roomId", s.cfg.RoomID)
	status, body, err := s.postForm("/data/apps/seatengine/sign", form)
	if err != nil {
		return "", err
	}
	_ = status
	return string(body), nil
}

// SignBack 退座：POST data/apps/seatengine/signback {id: reserveId}
func (s *Session) SignBack(reserveID int64) (string, error) {
	form := url.Values{}
	form.Set("id", strconv.FormatInt(reserveID, 10))
	status, body, err := s.postForm("/data/apps/seatengine/signback", form)
	if err != nil {
		return "", err
	}
	_ = status
	return string(body), nil
}

// RoomCapEnd 从 room/info 解析当日场地关闭时间。
// 优先级（与官方列表页一致）：特殊开放时间(按星期) > openTimeLongSettingJson > commonTimeConfig.<weekday>。
func (s *Session) RoomCapEnd() (string, error) {
	body, err := s.RoomInfo(s.cfg.RoomID)
	if err != nil {
		return "", err
	}
	var m struct {
		Data struct {
			SeatConfig struct {
				OpenTimeLongSettingJson struct {
					OpenTimeHourEnd string `json:"openTimeHourEnd"`
				} `json:"openTimeLongSettingJson"`
				CommonTimeConfig map[string]any `json:"commonTimeConfig"`
				Renewal          int            `json:"renewal"`
			} `json:"seatConfig"`
			SeatRoom struct {
				SpecialTime *struct {
					MonEnd  string `json:"monEndTime"`
					TuesEnd string `json:"tuesEndTime"`
					WedEnd  string `json:"wedEndTime"`
					ThurEnd string `json:"thurEndTime"`
					FriEnd  string `json:"friEndTime"`
					SatEnd  string `json:"satEndTime"`
					SunEnd  string `json:"sunEndTime"`
				} `json:"seatEngineSpecialTime"`
			} `json:"seatRoom"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return "", err
	}
	wk := []string{"sun", "mon", "tues", "wed", "thur", "fri", "sat"}
	dayKey := wk[int(time.Now().Weekday())]
	sc := m.Data.SeatConfig

	// 1. 特殊开放时间（优先）
	if st := m.Data.SeatRoom.SpecialTime; st != nil {
		var end string
		switch dayKey {
		case "sun":
			end = st.SunEnd
		case "mon":
			end = st.MonEnd
		case "tues":
			end = st.TuesEnd
		case "wed":
			end = st.WedEnd
		case "thur":
			end = st.ThurEnd
		case "fri":
			end = st.FriEnd
		case "sat":
			end = st.SatEnd
		}
		if end != "" {
			logf("关闭时间规则: 特殊开放时间(%s)=%s renewal=%d", dayKey, end, sc.Renewal)
			return end, nil
		}
	}
	// 2. 通用模板
	if sc.OpenTimeLongSettingJson.OpenTimeHourEnd != "" {
		logf("关闭时间规则: openTimeLongEnd=%s renewal=%d", sc.OpenTimeLongSettingJson.OpenTimeHourEnd, sc.Renewal)
		return sc.OpenTimeLongSettingJson.OpenTimeHourEnd, nil
	}
	// 3. 星期配置
	commonEnd := ""
	if v, ok := sc.CommonTimeConfig[dayKey+"EndTime"]; ok {
		commonEnd, _ = v.(string)
	}
	if commonEnd != "" {
		logf("关闭时间规则: common(%s)End=%s renewal=%d", dayKey, commonEnd, sc.Renewal)
		return commonEnd, nil
	}
	return "", fmt.Errorf("未找到场地关闭时间配置")
}

// MyReserves 查询当前预约列表：POST data/apps/seatengine/index {seatId, seatIdEnc}。
func (s *Session) MyReserves() (string, error) {
	form := url.Values{}
	form.Set("seatId", s.cfg.SeatID)
	form.Set("seatIdEnc", "9dffbb2440d6a600")
	status, body, err := s.postForm("/data/apps/seatengine/index", form)
	if err != nil {
		return "", err
	}
	_ = status
	return string(body), nil
}

// CancelReserve 取消预约：POST data/apps/seatengine/cancel {id: reserveId}
func (s *Session) CancelReserve(reserveID string) (string, error) {
	form := url.Values{}
	form.Set("id", reserveID)
	status, body, err := s.postForm("/data/apps/seatengine/cancel", form)
	if err != nil {
		return "", err
	}
	_ = status
	return string(body), nil
}

// GetUsedTimes 查询指定座位当天的已占用时间段。
func (s *Session) GetUsedTimes(seatNum, day string) (string, error) {
	form := url.Values{}
	form.Set("seatId", s.cfg.SeatID)
	form.Set("roomId", s.cfg.RoomID)
	form.Set("seatNum", padSeatNum(seatNum))
	form.Set("day", day)
	form.Set("verifyData", "1")
	status, body, err := s.postForm("/data/apps/seatengine/getusedtimes", form)
	if err != nil {
		return "", err
	}
	_ = status
	return string(body), nil
}

// CheckExist 查询是否存在未签退预约。
func (s *Session) CheckExist(seatNum string) (string, error) {
	form := url.Values{}
	form.Set("seatNum", padSeatNum(seatNum))
	form.Set("seatId", s.cfg.SeatID)
	form.Set("roomId", s.cfg.RoomID)
	status, body, err := s.postForm("/data/apps/seatengine/check/exist", form)
	if err != nil {
		return "", err
	}
	_ = status
	return string(body), nil
}

// ConfirmDialog 确认公告弹窗（模拟页面"开始预约"按钮）。
func (s *Session) ConfirmDialog() (string, error) {
	form := url.Values{}
	form.Set("seatId", s.cfg.SeatID)
	form.Set("dialogAgreeStatus", "1")
	status, body, err := s.postForm("/data/apps/seatengine/dialog", form)
	if err != nil {
		return "", err
	}
	_ = status
	return string(body), nil
}

// GetCaptchaType 查询验证码类型配置（appType=2 座位业务, appId=seatId）。
func (s *Session) GetCaptchaType() (string, error) {
	form := url.Values{}
	form.Set("appType", "2")
	form.Set("appId", s.cfg.SeatID)
	status, body, err := s.postForm("/data/apps/seat/captcha/type", form)
	if err != nil {
		return "", err
	}
	_ = status
	return string(body), nil
}

// GetRiskConfig 查询风控配置（是否开启网易易盾等）。
func (s *Session) GetRiskConfig() (string, error) {
	form := url.Values{}
	form.Set("appType", "2")
	form.Set("appId", s.cfg.SeatID)
	status, body, err := s.postForm("/data/apps/seat/risk/check/config", form)
	if err != nil {
		return "", err
	}
	_ = status
	return string(body), nil
}
// GetCaptcha 获取图片验证码地址（data.captchaUrl）。
func (s *Session) GetCaptcha() (string, error) {
	status, body, err := s.postForm("/data/apps/seatengine/captcha", url.Values{})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return string(body), fmt.Errorf("HTTP %d", status)
	}
	var m map[string]any
	if json.Unmarshal(body, &m) == nil {
		data, _ := m["data"].(map[string]any)
		if data != nil {
			if u, ok := data["captchaUrl"].(string); ok && u != "" {
				return u, nil
			}
			if u, ok := data["url"].(string); ok && u != "" {
				return u, nil
			}
		}
	}
	return string(body), nil
}

// IsReserveOK 判断预约响应是否成功。
func IsReserveOK(respText string) bool {
	t := strings.ToLower(respText)
	if strings.Contains(t, `"success":true`) {
		return true
	}
	if strings.Contains(t, "预约成功") || strings.Contains(t, "预订成功") {
		return true
	}
	return false
}

// DescribeCookies 输出当前会话 Cookie（调试用）。
func (s *Session) DescribeCookies() string {
	var parts []string
	if s.client.Jar != nil {
		for _, u := range []string{"https://passport2.chaoxing.com", "https://office.chaoxing.com"} {
			if uu, err := url.Parse(u); err == nil {
				for _, ck := range s.client.Jar.Cookies(uu) {
					parts = append(parts, ck.Name+"="+ck.Value)
				}
			}
		}
	}
	return strings.Join(parts, "; ")
}

func jsonFieldTrue(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t == 1
	case string:
		return t == "1" || strings.EqualFold(t, "true")
	}
	return false
}

func fieldStr(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		if f, ok := v.(float64); ok {
			return strconv.FormatFloat(f, 'f', -1, 64)
		}
	}
	return ""
}

func extractJSONField(jsonStr, key string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return ""
	}
	return fieldStr(m, key)
}

func padSeatNum(seatNum string) string {
	seatNum = strings.TrimSpace(seatNum)
	for len(seatNum) < 3 {
		seatNum = "0" + seatNum
	}
	return seatNum
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
