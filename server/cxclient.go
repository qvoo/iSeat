package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CXClient 超星办公系统客户端：登录会话 + 座位业务接口。
type CXClient struct {
	BaseOffice string
	LoginURL   string
	SeatID     string
	RoomID     string
	SeatNum    string
	DeptIDEnc  string // 学校/单位 deptIdEnc
	SeatIDEnc  string // 座位业务 seatIdEnc
	CaptchaID  string // 学校滑块验证码 captchaId
	UserAgent  string
	Debug      bool

	client *http.Client
}

// NewCXClient 创建客户端。
func NewCXClient(baseOffice, loginURL, seatID, roomID, seatNum string) *CXClient {
	jar, _ := cookiejar.New(nil)
	return &CXClient{
		BaseOffice: baseOffice,
		LoginURL:   loginURL,
		SeatID:     seatID,
		RoomID:     roomID,
		SeatNum:    seatNum,
		UserAgent:  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		client:     &http.Client{Jar: jar, Timeout: 30 * time.Second},
	}
}

// SetSchool 设置该客户端的学校参数（deptIdEnc / seatIdEnc / captchaId）。
func (c *CXClient) SetSchool(deptIDEnc, seatIDEnc, captchaID string) {
	if deptIDEnc != "" {
		c.DeptIDEnc = deptIDEnc
	}
	if seatIDEnc != "" {
		c.SeatIDEnc = seatIDEnc
	}
	if captchaID != "" {
		c.CaptchaID = captchaID
	}
}

func (c *CXClient) applyHeaders(req *http.Request) {
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
}

func (c *CXClient) do(req *http.Request) (int, []byte, error) {
	c.applyHeaders(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, body, err
}

func (c *CXClient) postForm(path string, form url.Values) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, c.BaseOffice+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	c.applyHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	return c.do(req)
}

// Login 使用 chaoxing 账号密码登录（fanyalogin）。
func (c *CXClient) Login(username, password string) error {
	uname, err := cxAES(username)
	if err != nil {
		return err
	}
	pwd, err := cxAES(password)
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("fid", "-1")
	form.Set("uname", uname)
	form.Set("password", pwd)
	form.Set("refer", c.BaseOffice+"/front/apps/seatengine/index?seatId="+c.SeatID)
	form.Set("t", "true")
	form.Set("forbidotherlogin", "0")
	form.Set("validate", "")
	form.Set("doubleFactorLogin", "0")
	form.Set("independentId", "0")
	req, _ := http.NewRequest(http.MethodPost, c.LoginURL, strings.NewReader(form.Encode()))
	c.applyHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://passport2.chaoxing.com")
	_, body, err := c.do(req)
	if err != nil {
		return err
	}
	txt := string(body)
	if strings.Contains(txt, `"status":true`) || strings.Contains(txt, `"result":1`) || strings.Contains(txt, `"success":true`) {
		return nil
	}
	msg := cxField(txt, "msg")
	if msg == "" {
		msg = truncate(txt, 200)
	}
	return fmt.Errorf("登录失败: %s", msg)
}

var (
	reRoomID    = regexp.MustCompile(`(?i)var\s+roomId\s*=\s*['"]([0-9]+)['"]`)
	reSeatNum   = regexp.MustCompile(`(?i)var\s+seatNum\s*=\s*['"](\d+)['"]`)
	reSeatID    = regexp.MustCompile(`(?i)var\s+seatId\s*=\s*['"](\d+)['"]`)
	reSubmitEnc = regexp.MustCompile(`(?i)id="submit_enc"\s+value="([^"]+)"`)
)

// CodePage 座位页字段。
type CodePage struct {
	RoomID    string
	SeatNum   string
	SeatID    string
	SubmitEnc string
}

// FetchCodePage 获取座位页并解析 submit_enc。
func (c *CXClient) FetchCodePage(roomID, seatNum, seatID string) (*CodePage, error) {
	u := fmt.Sprintf("%s/front/apps/seatengine/code?id=%s&seatNum=%s&seatId=%s",
		c.BaseOffice, url.QueryEscape(roomID), url.QueryEscape(seatNum), url.QueryEscape(seatID))
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	c.applyHeaders(req)
	_, body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	txt := string(body)
	cp := &CodePage{RoomID: roomID, SeatNum: seatNum, SeatID: seatID}
	if m := reRoomID.FindStringSubmatch(txt); len(m) == 2 {
		cp.RoomID = m[1]
	}
	if m := reSeatNum.FindStringSubmatch(txt); len(m) == 2 {
		cp.SeatNum = m[1]
	}
	if m := reSeatID.FindStringSubmatch(txt); len(m) == 2 {
		cp.SeatID = m[1]
	}
	if m := reSubmitEnc.FindStringSubmatch(txt); len(m) == 2 {
		cp.SubmitEnc = m[1]
	} else {
		return nil, fmt.Errorf("页面未找到 submit_enc")
	}
	return cp, nil
}

// Reserve 提交预约。day=YYYY-MM-DD，start/end=HH:MM，captchaToken 为滑块 validate。
func (c *CXClient) Reserve(day, start, end, seatNum, captchaToken, submitEnc string) (string, error) {
	seatNum = padSeat(seatNum)
	params := [][2]string{
		{"roomId", c.RoomID},
		{"day", day},
		{"startTime", start},
		{"endTime", end},
		{"seatNum", seatNum},
		{"captcha", captchaToken},
		{"type", "1"},
		{"verifyData", "1"},
		{"wyToken", ""},
	}
	enc := cxEnc(params, submitEnc)
	form := url.Values{}
	for _, p := range params {
		form.Set(p[0], p[1])
	}
	form.Set("enc", enc)
	_, body, err := c.postForm("/data/apps/seatengine/submit", form)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// ReserveResult 提交返回。
type ReserveResult struct {
	Success bool  `json:"success"`
	Msg     string `json:"msg"`
	ID      int64  `json:"-"`
	EndAt   int64  `json:"-"`
}

// ParseReserve 解析预约响应。返回 (reserveId, endAtMs, 原响应)。与 status=0 校验。
func (c *CXClient) ParseReserve(respText string) (int64, int64, error) {
	var m struct {
		Success bool `json:"success"`
		Msg     string `json:"msg"`
		Data    struct {
			SeatReserve struct {
				ID      int64 `json:"id"`
				EndTime int64 `json:"endTime"`
			} `json:"seatReserve"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(respText), &m); err != nil {
		return 0, 0, err
	}
	if !m.Success {
		return 0, 0, fmt.Errorf("预约失败: %s", m.Msg)
	}
	if m.Data.SeatReserve.ID == 0 {
		return 0, 0, fmt.Errorf("响应缺少预约 id: %s", truncate(respText, 300))
	}
	return m.Data.SeatReserve.ID, m.Data.SeatReserve.EndTime, nil
}

// SignIn 签到。
func (c *CXClient) SignIn(reserveID int64) (string, error) {
	form := url.Values{}
	form.Set("id", strconv.FormatInt(reserveID, 10))
	form.Set("seatId", c.SeatID)
	form.Set("roomId", c.RoomID)
	_, body, err := c.postForm("/data/apps/seatengine/sign", form)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// SignBack 退座。
func (c *CXClient) SignBack(reserveID int64) (string, error) {
	form := url.Values{}
	form.Set("id", strconv.FormatInt(reserveID, 10))
	_, body, err := c.postForm("/data/apps/seatengine/signback", form)
	return string(body), err
}

// CancelReserve 取消预约。
func (c *CXClient) CancelReserve(reserveID int64) (string, error) {
	form := url.Values{}
	form.Set("id", strconv.FormatInt(reserveID, 10))
	_, body, err := c.postForm("/data/apps/seatengine/cancel", form)
	return string(body), err
}

// ReserveInfo 当前预约。
type ReserveInfo struct {
	ID        int64  `json:"id"`
	Status    int    `json:"status"`
	StartTime int64  `json:"startTime"`
	EndTime   int64  `json:"endTime"`
	SeatNum   string `json:"seatNum"`
	RoomID    int64  `json:"roomId"`
	RoomName  string `json:"-"`
	First     string `json:"firstLevelName"`
	Second    string `json:"secondLevelName"`
	Third     string `json:"thirdLevelName"`
	Today     string `json:"today"`
}

// RoomIDStr 房间ID字符串。
func (r *ReserveInfo) RoomIDStr() string {
	return strconv.FormatInt(r.RoomID, 10)
}

// MyReserves 查询当前/近期预约。
func (c *CXClient) MyReserves(seatID string) (cur []ReserveInfo, near []ReserveInfo, err error) {
	form := url.Values{}
	form.Set("seatId", seatID)
	if c.SeatIDEnc != "" {
		form.Set("seatIdEnc", c.SeatIDEnc)
	} else {
		form.Set("seatIdEnc", "9dffbb2440d6a600")
	}
	_, body, err := c.postForm("/data/apps/seatengine/index", form)
	if err != nil {
		return nil, nil, err
	}
	var m struct {
		Data struct {
			CurReserves  []ReserveInfo `json:"curReserves"`
			NearReserves []ReserveInfo `json:"nearReserves"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, nil, err
	}
	return m.Data.CurReserves, m.Data.NearReserves, nil
}

// RoomInfo 房间信息原始响应。
func (c *CXClient) RoomInfo(roomID string) ([]byte, error) {
	form := url.Values{}
	form.Set("id", roomID)
	_, body, err := c.postForm("/data/apps/seatengine/room/info", form)
	return body, err
}

// RoomCapEnd 房间闭馆时间（遍历 room/info 的开放时间规则，按星期）。
func (c *CXClient) RoomCapEnd(roomID string) (string, error) {
	return c.RoomCapEndAt(roomID, time.Now())
}

// RoomCapEndAt 按指定日期取房间闭馆时间。
// 遍历优先级（与官方列表页 reLoadData 一致）：
//  1. seatRoom.seatEngineSpecialTime 特殊开放时间（某座位/房间按星期）
//  2. seatConfig.openTimeLongSettingJson.openTimeHourEnd
//  3. seatConfig.commonTimeConfig.<星期>EndTime
func (c *CXClient) RoomCapEndAt(roomID string, t time.Time) (string, error) {
	body, err := c.RoomInfo(roomID)
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
			} `json:"seatConfig"`
			SeatRoom struct {
				SpecialTime *struct {
					MonEnd   string `json:"monEndTime"`
					TuesEnd  string `json:"tuesEndTime"`
					WedEnd   string `json:"wedEndTime"`
					ThurEnd  string `json:"thurEndTime"`
					FriEnd   string `json:"friEndTime"`
					SatEnd   string `json:"satEndTime"`
					SunEnd   string `json:"sunEndTime"`
				} `json:"seatEngineSpecialTime"`
			} `json:"seatRoom"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return "", err
	}
	weekdayKeys := []string{"sun", "mon", "tues", "wed", "thur", "fri", "sat"}
	dayKey := weekdayKeys[int(t.Weekday())]

	// 1. 特殊开放时间（优先）
	if st := m.Data.SeatRoom.SpecialTime; st != nil {
		end, err := fieldByDay(dayKey, st.MonEnd, st.TuesEnd, st.WedEnd, st.ThurEnd, st.FriEnd, st.SatEnd, st.SunEnd)
		if err == nil {
			log.Printf("[闭馆遍历] room=%s %s 特殊开放时间=%s", roomID, dayKey, end)
			return end, nil
		}
	}
	// 2. 通用模板
	sc := m.Data.SeatConfig
	if sc.OpenTimeLongSettingJson.OpenTimeHourEnd != "" {
		return sc.OpenTimeLongSettingJson.OpenTimeHourEnd, nil
	}
	// 3. 星期配置
	commonEnd := ""
	if v, ok := sc.CommonTimeConfig[dayKey+"EndTime"]; ok {
		commonEnd, _ = v.(string)
	}
	if commonEnd != "" {
		return commonEnd, nil
	}
	return "", fmt.Errorf("未获取到房间关闭时间")
}

func fieldByDay(day string, mon, tues, wed, thur, fri, sat, sun string) (string, error) {
	switch day {
	case "sun":
		return sun, nil
	case "mon":
		return mon, nil
	case "tues":
		return tues, nil
	case "wed":
		return wed, nil
	case "thur":
		return thur, nil
	case "fri":
		return fri, nil
	case "sat":
		return sat, nil
	}
	return "", fmt.Errorf("未知星期 %s", day)
}

// SeatCell 座位格子。
type SeatCell struct {
	Num       string `json:"num"`
	Available bool   `json:"available"`
	Occupied  bool   `json:"occupied"` // 该时间段已被预约
	Disabled  bool   `json:"disabled"` // 区域暂停预约(isReserve=0)
}

// RoomSeats 获取房间座位网格状态：数字网格(startSeatNum~capacity) + 占用 + 暂停。
func (c *CXClient) RoomSeats(seatID, roomID, day, start, end string) ([]SeatCell, error) {
	body, err := c.RoomInfo(roomID)
	if err != nil {
		return nil, err
	}
	var m struct {
		Data struct {
			SeatRoom struct {
				StartSeatNum int `json:"startSeatNum"`
				Capacity     int `json:"capacity"`
			} `json:"seatRoom"`
			SeatAttributes []struct {
				SeatNum   int `json:"seatNum"`
				IsReserve int `json:"isReserve"`
			} `json:"seatAttributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	capacity := m.Data.SeatRoom.Capacity
	startSeat := m.Data.SeatRoom.StartSeatNum
	if startSeat == 0 {
		startSeat = 1
	}
	if capacity <= 0 || capacity > 500 {
		return nil, fmt.Errorf("座位数异常: %d", capacity)
	}

	// 占用集合
	occupied := map[string]bool{}
	form := url.Values{}
	form.Set("seatId", seatID)
	form.Set("roomId", roomID)
	form.Set("startTime", start)
	form.Set("endTime", end)
	form.Set("day", day)
	if _, b, err := c.postForm("/data/apps/seatengine/getusedseatnums", form); err == nil {
		var used struct {
			Data struct {
				SeatReserves []struct {
					SeatNum string `json:"seatNum"`
				} `json:"seatReserves"`
			} `json:"data"`
		}
		if json.Unmarshal(b, &used) == nil {
			for _, r := range used.Data.SeatReserves {
				occupied[padSeat(r.SeatNum)] = true
			}
		}
	}

	// 暂停集合
	paused := map[string]bool{}
	for _, a := range m.Data.SeatAttributes {
		if a.IsReserve == 0 {
			paused[padSeat(strconv.Itoa(a.SeatNum))] = true
		}
	}

	var out []SeatCell
	for i := 0; i < capacity; i++ {
		num := padSeat(strconv.Itoa(startSeat + i))
		cell := SeatCell{Num: num}
		if occupied[num] {
			cell.Occupied = true
		}
		if paused[num] {
			cell.Disabled = true
		}
		cell.Available = !cell.Occupied && !cell.Disabled
		out = append(out, cell)
	}
	return out, nil
}

// RoomItem 自习室列表项。
type RoomItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Floor1    string `json:"floor1"`
	Floor2    string `json:"floor2"`
	Floor3    string `json:"floor3"`
	CapEnd    string `json:"cap_end"`
	OpenTime  string `json:"open_time"`
	Capacity  int    `json:"capacity"`
	IsOpen    int    `json:"is_open"`
}

// RoomList 全部自习室列表（room/list 分页拉取）。
func (c *CXClient) RoomList(seatID, day string) ([]RoomItem, error) {
	var out []RoomItem
	page := 1
	const pageSize = 60
	for {
		form := url.Values{}
		form.Set("day", day)
		if c.DeptIDEnc != "" {
			form.Set("deptIdEnc", c.DeptIDEnc)
		} else {
			form.Set("deptIdEnc", "0fd2b43990df8985")
		}
		form.Set("seatId", seatID)
		form.Set("cpage", strconv.Itoa(page))
		form.Set("pageSize", strconv.Itoa(pageSize))
		_, body, err := c.postForm("/data/apps/seatengine/room/list", form)
		if err != nil {
			return nil, err
		}
		var m struct {
			Data struct {
				SeatRoomList []struct {
					ID              int64  `json:"id"`
					FirstLevelName  string `json:"firstLevelName"`
					SecondLevelName string `json:"secondLevelName"`
					ThirdLevelName  string `json:"thirdLevelName"`
					Capacity        int    `json:"capacity"`
					IsShow          int    `json:"isShow"`
					SpecialTime     *struct {
						MonOpen  int    `json:"monOpen"`
						MonStart string `json:"monStartTime"`
						MonEnd   string `json:"monEndTime"`
						TuesOpen  int    `json:"tuesOpen"`
						TuesEnd   string `json:"tuesEndTime"`
						WedOpen   int    `json:"wedOpen"`
						WedEnd    string `json:"wedEndTime"`
						ThurOpen  int    `json:"thurOpen"`
						ThurEnd   string `json:"thurEndTime"`
						FriOpen   int    `json:"friOpen"`
						FriEnd    string `json:"friEndTime"`
						SatOpen   int    `json:"satOpen"`
						SatEnd    string `json:"satEndTime"`
						SunOpen   int    `json:"sunOpen"`
						SunEnd    string `json:"sunEndTime"`
					} `json:"seatEngineSpecialTime"`
				} `json:"seatRoomList"`
				TotalPage int `json:"totalPage"`
			} `json:"data"`
			Success bool `json:"success"`
		}
		if err := json.Unmarshal(body, &m); err != nil {
			return nil, err
		}
		if !m.Success {
			return nil, fmt.Errorf("room/list 失败: %s", truncate(string(body), 200))
		}
		for _, r := range m.Data.SeatRoomList {
			item := RoomItem{
				ID:       strconv.FormatInt(r.ID, 10),
				Name:     r.FirstLevelName + "-" + r.SecondLevelName + "-" + r.ThirdLevelName,
				Floor1:   r.FirstLevelName,
				Floor2:   r.SecondLevelName,
				Floor3:   r.ThirdLevelName,
				Capacity: r.Capacity,
				IsOpen:   r.IsShow,
			}
			if r.SpecialTime != nil {
				item.CapEnd, item.OpenTime = weekdayOpenClose(r.SpecialTime)
			}
			out = append(out, item)
		}
		if page >= m.Data.TotalPage || len(m.Data.SeatRoomList) < pageSize {
			break
		}
		page++
	}
	return out, nil
}

func weekdayIdx() int {
	return int(time.Now().Weekday())
}

func weekdayOpenClose(st *struct {
	MonOpen  int    `json:"monOpen"`
	MonStart string `json:"monStartTime"`
	MonEnd   string `json:"monEndTime"`
	TuesOpen  int    `json:"tuesOpen"`
	TuesEnd   string `json:"tuesEndTime"`
	WedOpen   int    `json:"wedOpen"`
	WedEnd    string `json:"wedEndTime"`
	ThurOpen  int    `json:"thurOpen"`
	ThurEnd   string `json:"thurEndTime"`
	FriOpen   int    `json:"friOpen"`
	FriEnd    string `json:"friEndTime"`
	SatOpen   int    `json:"satOpen"`
	SatEnd    string `json:"satEndTime"`
	SunOpen   int    `json:"sunOpen"`
	SunEnd    string `json:"sunEndTime"`
}) (end, start string) {
	items := []struct {
		open  int
		start string
		end   string
	}{
		{st.SunOpen, "", st.SunEnd},
		{st.MonOpen, st.MonStart, st.MonEnd},
		{st.TuesOpen, "", st.TuesEnd},
		{st.WedOpen, "", st.WedEnd},
		{st.ThurOpen, "", st.ThurEnd},
		{st.FriOpen, "", st.FriEnd},
		{st.SatOpen, "", st.SatEnd},
	}
	i := weekdayIdx()
	return items[i].end, items[i].start
}

// ------------- 加密/签名 -------------

const cxAESKey = "u2oh6Vu^HWe4_AES"

func cxAES(plain string) (string, error) {
	key := []byte(cxAESKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	data := []byte(plain)
	pad := aes.BlockSize - len(data)%aes.BlockSize
	data = append(data, bytes.Repeat([]byte{byte(pad)}, pad)...)
	ct := make([]byte, len(data))
	cipher.NewCBCEncrypter(block, key).CryptBlocks(ct, data)
	return base64.StdEncoding.EncodeToString(ct), nil
}

func cxEnc(params [][2]string, submitEnc string) string {
	sorted := make([][2]string, len(params))
	copy(sorted, params)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i][0] < sorted[j][0] })
	var sb strings.Builder
	for _, p := range sorted {
		sb.WriteByte('[')
		sb.WriteString(p[0])
		sb.WriteByte('=')
		sb.WriteString(p[1])
		sb.WriteByte(']')
	}
	sb.WriteByte('[')
	sb.WriteString(submitEnc)
	sb.WriteByte(']')
	sum := md5.Sum([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

func padSeat(s string) string {
	s = strings.TrimSpace(s)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

func cxField(jsonStr, key string) string {
	var m map[string]any
	if json.Unmarshal([]byte(jsonStr), &m) != nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
