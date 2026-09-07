package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// saveCaptchaImage 将 captcha 接口返回的 data URI 保存为图片文件。
func saveCaptchaImage(content, path string) (string, bool) {
	if !strings.HasPrefix(content, "data:image") {
		return "", false
	}
	idx := strings.Index(content, "base64,")
	if idx < 0 {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(content[idx+len("base64,"):])
	if err != nil {
		return "", false
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", false
	}
	return path, true
}

// today 返回预约日期，配置 Day 为空时使用当天。
func today(cfg *Config) string {
	if cfg.Day != "" {
		return cfg.Day
	}
	return time.Now().Format("2006-01-02")
}

// seatCandidates 返回按优先级排序的候选座位号集合。
func seatCandidates(cfg *Config, override []string) []string {
	seats := []string{strings.TrimSpace(cfg.SeatNum)}
	seats = append(seats, cfg.AltSeats...)
	if len(override) > 0 {
		seats = override
	}
	var out []string
	for _, s := range seats {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// doBookOnce 执行一次完整预约流程（登录->取座位页->提交预约）。
// 若配置 auto_captcha 且未提供 token, 会自动解滑块验证码。
// dayStr 为空时自动取当天；否则按指定日期提交。
func doBookOnce(cfg *Config, start, end string, overrideSeats []string, captchaToken, dayStr string, forever bool) bool {
	sess, err := NewSession(cfg)
	if err != nil {
		errf("创建会话失败: %v", err)
		return false
	}
	if _, err := sess.Login(); err != nil {
		errf("%v", err)
		return false
	}

	day := dayStr
	if day == "" {
		day = today(cfg)
	}
	seats := seatCandidates(cfg, overrideSeats)
	solver := NewCXCaptcha(cfg.BaseOffice + "/front/apps/seatengine/code?id=" + cfg.RoomID + "&seatNum=" + cfg.SeatNum + "&seatId=" + cfg.SeatID)
	attempt := 0
	for {
		attempt++
		// 每次尝试重新取座位页，保证 submit_enc 最新
		cp, err := sess.FetchCodePage()
		if err != nil {
			errf("获取座位页失败: %v", err)
			if cfg.MaxRetry > 0 && attempt >= cfg.MaxRetry {
				return false
			}
			sleepMS(cfg.RequestIntervalMs)
			continue
		}
		// 需要验证码时自动求解
		token := captchaToken
		if cfg.AutoCaptcha && token == "" {
			logf("第%d轮: 开始解滑块验证码...", attempt)
			res, serr := solver.Solve(7)
			if serr != nil {
				errf("验证码求解失败: %v", serr)
				if cfg.MaxRetry > 0 && attempt >= cfg.MaxRetry {
					return false
				}
				sleepMS(cfg.RequestIntervalMs * 2)
				continue
			}
			token = res.Validate
			logf("验证码求解成功: %s (x=%d)", token, res.BestX)
		}
		for _, seat := range seats {
			resp, rerr := sess.SubmitReserve(day, start, end, seat, token, cp.SubmitEnc)
			if rerr != nil {
				warnf("座位 %s 请求异常: %v", seat, rerr)
				continue
			}
			if IsReserveOK(resp) {
				logf("========== 预约成功 ==========")
				logf("座位: %s  时间: %s ~ %s  日期: %s", padSeatNum(seat), start, end, day)
				logf("服务器响应: %s", truncate(resp, 300))
				return true
			}
			logf("座位 %s 抢座失败 (第%d轮): %s", padSeatNum(seat), attempt, truncate(resp, 300))
			if strings.Contains(resp, "303") {
				// 验证码失效: 下一轮重新求解
				token = ""
			}
		}
		if cfg.MaxRetry > 0 && attempt >= cfg.MaxRetry {
			errf("达到最大尝试次数 %d，放弃", cfg.MaxRetry)
			return false
		}
		if !forever {
			return false
		}
		sleepMS(cfg.RequestIntervalMs)
	}
}

// cmdSubmitX 测试提交变体（调试用）。
func cmdSubmitX(cfg *Config, start, end string, variant int) {
	sess, err := NewSession(cfg)
	if err != nil {
		errf("创建会话失败: %v", err)
		os.Exit(1)
	}
	if _, err := sess.Login(); err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	cp, err := sess.FetchCodePage()
	if err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	day := today(cfg)
	solver := NewCXCaptcha(cfg.BaseOffice + "/front/apps/seatengine/code?id=" + cfg.RoomID + "&seatNum=" + cfg.SeatNum + "&seatId=" + cfg.SeatID)
	res, serr := solver.Solve(7)
	if serr != nil {
		errf("验证码求解失败: %v", serr)
		os.Exit(1)
	}
	logf("验证码: %s (x=%d)", res.Validate, res.BestX)
	resp, serr := sess.SubmitReserveEx(day, start, end, cfg.SeatNum, res.Validate, cp.SubmitEnc, variant)
	if serr != nil {
		errf("submit 异常: %v", serr)
		os.Exit(1)
	}
	logf("submit(variant=%d) 响应: %s", variant, resp)
	if IsReserveOK(resp) {
		logf("预约成功！")
	} else {
		os.Exit(1)
	}
}

// cmdQR 解码二维码图片内容。
func cmdQR(path string) {
	text, err := DecodeQRFile(path)
	if err != nil {
		errf("二维码解码失败: %v", err)
		os.Exit(1)
	}
	logf("二维码内容: %s", text)
}

// cmdSign 自动签到。reserveID 可空（自动找待签到预约）；qrPath 可空（识别桌上的
// 座位二维码照片，用于校验座位信息）。
func cmdSign(cfg *Config, reserveID int64, qrPath string) {
	if qrPath != "" {
		text, derr := DecodeQRFile(qrPath)
		if derr != nil {
			warnf("二维码识别失败(不影响签到): %v", derr)
		} else {
			logf("二维码内容: %s", text)
		}
	}
	sess, err := NewSession(cfg)
	if err != nil {
		errf("创建会话失败: %v", err)
		os.Exit(1)
	}
	if _, err := sess.Login(); err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	id := reserveID
	if id == 0 {
		list, lerr := sess.ListCurReserves()
		if lerr != nil {
			errf("查询预约失败: %v", lerr)
			os.Exit(1)
		}
		for _, r := range list {
			if r.Status == 0 || r.Status == 9 {
				id = r.ID
				logf("找到待签到预约: 座位%s %s~%s (id=%d)", r.SeatNum,
					time.UnixMilli(r.StartTime).Format("15:04"), time.UnixMilli(r.EndTime).Format("15:04"), r.ID)
				break
			}
		}
		if id == 0 {
			errf("没有待签到的预约 (当前状态: %s)", formatReserveStatuses(list))
			os.Exit(1)
		}
	}
	resp, serr := sess.SignIn(id)
	if serr != nil {
		errf("签到请求失败: %v", serr)
		os.Exit(1)
	}
	logf("签到响应: %s", resp)
	if IsReserveOK(resp) {
		logf("====== 签到成功！座位已生效 ======")
	} else {
		os.Exit(1)
	}
}

func formatReserveStatuses(list []ReserveInfo) string {
	var parts []string
	for _, r := range list {
		parts = append(parts, fmt.Sprintf("座位%s status=%d", r.SeatNum, r.Status))
	}
	if len(parts) == 0 {
		return "无预约"
	}
	return strings.Join(parts, ", ")
}

// ensureSign 等待签到窗口并自动签到（watch 模式使用）。
func ensureSign(cfg *Config, sess *Session) bool {
	preSign := time.Duration(cfg.SignAheadMinute) * time.Minute
	if preSign <= 0 {
		preSign = 20 * time.Minute
	}
	for {
		list, err := sess.ListCurReserves()
		if err != nil {
			errf("查询预约失败: %v", err)
			time.Sleep(30 * time.Second)
			continue
		}
		var pending *ReserveInfo
		for i := range list {
			if list[i].Status == 0 || list[i].Status == 9 {
				pending = &list[i]
				break
			}
		}
		if pending == nil {
			for i := range list {
				if list[i].Status == 1 || list[i].Status == 3 || list[i].Status == 5 {
					logf("座位已在使用中，无需再签到")
					return true
				}
			}
			warnf("未找到待签到预约，继续等待...")
			time.Sleep(30 * time.Second)
			continue
		}
		signAt := time.UnixMilli(pending.StartTime).Add(-preSign)
		now := time.Now()
		if now.Before(signAt) {
			logf("签到窗口将于 %s 开启 (预约 %s 开始)，等待中...", signAt.Format("15:04"), time.UnixMilli(pending.StartTime).Format("15:04"))
			time.Sleep(time.Until(signAt.Add(2 * time.Second)))
		}
		resp, serr := sess.SignIn(pending.ID)
		if serr != nil {
			errf("签到请求失败: %v", serr)
			time.Sleep(30 * time.Second)
			continue
		}
		if IsReserveOK(resp) {
			logf("====== 自动签到成功！座位 %s 使用中 ======", pending.SeatNum)
			return true
		}
		warnf("签到未成功: %s (10秒后重试)", truncate(resp, 200))
		time.Sleep(10 * time.Second)
	}
}

// cmdSignBack 退座：结束当前使用。
func cmdSignBack(cfg *Config, reserveID int64) {
	sess, err := NewSession(cfg)
	if err != nil {
		errf("创建会话失败: %v", err)
		os.Exit(1)
	}
	if _, err := sess.Login(); err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	id := reserveID
	if id == 0 {
		list, lerr := sess.ListCurReserves()
		if lerr != nil {
			errf("查询预约失败: %v", lerr)
			os.Exit(1)
		}
		for _, r := range list {
			if r.Status == 1 || r.Status == 3 || r.Status == 5 {
				id = r.ID
				logf("找到使用中预约: 座位%s ~ %s (id=%d)", r.SeatNum, time.UnixMilli(r.EndTime).Format("15:04"), r.ID)
				break
			}
		}
	}
	if id == 0 {
		errf("没有使用中的预约")
		os.Exit(1)
	}
	resp, serr := sess.SignBack(id)
	if serr != nil {
		errf("退座请求失败: %v", serr)
		os.Exit(1)
	}
	logf("退座响应: %s", resp)
	if IsReserveOK(resp) {
		logf("====== 退座成功 ======")
	} else {
		os.Exit(1)
	}
}

// latestReserveEnd 查询最新一段有效预约的结束时间（跨天连续占座用）。
func latestReserveEnd(cfg *Config) (time.Time, bool, error) {
	sess, err := NewSession(cfg)
	if err != nil {
		return time.Time{}, false, err
	}
	if _, err := sess.Login(); err != nil {
		return time.Time{}, false, err
	}
	list, err := sess.ListCurReserves()
	if err != nil {
		return time.Time{}, false, err
	}
	var maxEnd time.Time
	for _, r := range list {
		switch r.Status {
		case 0, 1, 3, 5, 9:
			e := time.UnixMilli(r.EndTime)
			if e.After(maxEnd) {
				maxEnd = e
			}
		}
	}
	return maxEnd, !maxEnd.IsZero(), nil
}

// cmdMyReserves 查询当前预约及状态。
func cmdMyReserves(cfg *Config) {
	sess, err := NewSession(cfg)
	if err != nil {
		errf("创建会话失败: %v", err)
		os.Exit(1)
	}
	if _, err := sess.Login(); err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	resp, err := sess.MyReserves()
	if err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	logf("我的预约(原始响应): %s", resp)
}

// cmdCancel 取消指定预约（id 为 seatReserve.id）。
func cmdCancel(cfg *Config, reserveID string) {
	sess, err := NewSession(cfg)
	if err != nil {
		errf("创建会话失败: %v", err)
		os.Exit(1)
	}
	if _, err := sess.Login(); err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	resp, err := sess.CancelReserve(reserveID)
	if err != nil {
		errf("取消失败: %v", err)
		os.Exit(1)
	}
	logf("取消预约响应: %s", resp)
}

// cmdCodePage 抓取带自定义参数的 code 页（调试用）。
func cmdCodePage(cfg *Config, extra string) {
	sess, err := NewSession(cfg)
	if err != nil {
		errf("创建会话失败: %v", err)
		os.Exit(1)
	}
	if _, err := sess.Login(); err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	u := cfg.BaseOffice + "/front/apps/seatengine/code?id=" + cfg.RoomID +
		"&seatNum=" + cfg.SeatNum + "&seatId=" + cfg.SeatID + extra
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	sess.applyHeaders(req)
	status, body, err := sess.do(req)
	if err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	_ = os.WriteFile("code_page_sign.html", body, 0o644)
	logf("code 页面已保存 (HTTP %d, %d 字节)", status, len(body))
}

// cmdListPage 抓取"我的预约"列表页。
func cmdListPage(cfg *Config) {
	sess, err := NewSession(cfg)
	if err != nil {
		errf("创建会话失败: %v", err)
		os.Exit(1)
	}
	if _, err := sess.Login(); err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	u := cfg.BaseOffice + "/front/apps/seatengine/reserve/list?seatId=" + cfg.SeatID + "&wfwEngineEnc="
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	sess.applyHeaders(req)
	status, body, err := sess.do(req)
	if err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	_ = os.WriteFile("reserve_list.html", body, 0o644)
	logf("预约列表页已保存 (HTTP %d, %d 字节)", status, len(body))
}

// cmdFetchPath 探测任意路径（调试用）。-captcha 传 "POST|path|k=v&k2=v2"。
// path 以 http(s):// 开头时使用绝对地址（跨站探测，如 passport.chaoxing.com）。
func cmdFetchPath(cfg *Config, spec string) {
	sess, err := NewSession(cfg)
	if err != nil {
		errf("创建会话失败: %v", err)
		os.Exit(1)
	}
	if _, err := sess.Login(); err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	method := "GET"
	path := spec
	form := url.Values{}
	if i := strings.Index(spec, "|"); i > 0 {
		method = spec[:i]
		rest := spec[i+1:]
		if j := strings.Index(rest, "|"); j > 0 {
			path = rest[:j]
			if q, qerr := url.ParseQuery(rest[j+1:]); qerr == nil {
				for k, vs := range q {
					for _, v := range vs {
						form.Set(k, v)
					}
				}
			}
		} else {
			path = rest
		}
	}
	if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "http") {
		path = "/" + path
	}
	var body []byte
	var status int
	if method == "POST" {
		if strings.HasPrefix(path, "http") {
			req, rerr := http.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
			if rerr != nil {
				errf("%v", rerr)
				os.Exit(1)
			}
			sess.applyHeaders(req)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			status, body, err = sess.doRaw(req)
		} else {
			status, body, err = sess.postForm(path, form)
		}
	} else {
		u := cfg.BaseOffice + path
		if strings.HasPrefix(path, "http") {
			u = path
		}
		req, rerr := http.NewRequest(http.MethodGet, u, nil)
		if rerr != nil {
			errf("%v", rerr)
			os.Exit(1)
		}
		sess.applyHeaders(req)
		status, body, err = sess.do(req)
	}
	if err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	logf("路径 %s %s -> HTTP %d: %s", method, path, status, truncate(string(body), 600))
	_ = os.WriteFile("fetch_debug.html", body, 0o644)
}

// cmdIndex 抓取登录后的预约首页 HTML（调试用，用于查找退座/签到接口）。
func cmdIndex(cfg *Config) {
	sess, err := NewSession(cfg)
	if err != nil {
		errf("创建会话失败: %v", err)
		os.Exit(1)
	}
	if _, err := sess.Login(); err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	req, _ := http.NewRequest(http.MethodGet, cfg.BaseOffice+"/front/apps/seatengine/index?seatId="+cfg.SeatID, nil)
	sess.applyHeaders(req)
	req.Header.Set("Referer", "https://passport2.chaoxing.com/")
	status, body, err := sess.do(req)
	if err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	_ = os.WriteFile("index_page.html", body, 0o644)
	logf("index 页面已保存 (HTTP %d, %d 字节)", status, len(body))
}

// cmdCaptcha 独立测试滑块验证码求解。
func cmdCaptcha(cfg *Config) {
	solver := NewCXCaptcha(cfg.BaseOffice + "/front/apps/seatengine/code?id=" + cfg.RoomID + "&seatNum=" + cfg.SeatNum + "&seatId=" + cfg.SeatID)
	res, err := solver.Solve(7)
	if err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	logf("滑块验证成功! validate=%s (x=%d)", res.Validate, res.BestX)
	_ = os.WriteFile("validate_token.txt", []byte(res.Validate), 0o644)
	logf("已写入 validate_token.txt")
}

// cmdCheck 连通性检查：登录 + 座位页 + 房间信息（不预约）。
func cmdCheck(cfg *Config) {
	sess, err := NewSession(cfg)
	if err != nil {
		errf("创建会话失败: %v", err)
		os.Exit(1)
	}
	logf("尝试登录 %s ...", cfg.LoginURL)
	if _, err := sess.Login(); err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	cp, err := sess.FetchCodePage()
	if err != nil {
		logf("座位页解析失败(不影响登录，可能是页面问题): %v", err)
	}
	body, err := sess.RoomInfo(cfg.RoomID)
	if err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	sum := summarizeRoomInfo(body)
	logf("连接检查成功。房间: %s 座位: %s submit_enc: %s", cfg.RoomID, cfg.SeatNum, cp.SubmitEnc)
	logf("房间信息概要: %s", sum)
	if capEnd, cerr := sess.RoomCapEnd(); cerr == nil {
		logf("自习室关闭时间(占座至闭馆): %s", capEnd)
	} else {
		warnf("解析关闭时间失败: %v", cerr)
	}
}

// cmdInfo 输出房间完整 JSON 到 room_info.json（便于人工分析时段规则）。
func cmdInfo(cfg *Config) {
	sess, err := NewSession(cfg)
	if err != nil {
		errf("创建会话失败: %v", err)
		os.Exit(1)
	}
	if _, err := sess.Login(); err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	if _, err := sess.FetchCodePage(); err != nil {
		warnf("座位页解析警告: %v", err)
	}
	body, err := sess.RoomInfo(cfg.RoomID)
	if err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	if err := os.WriteFile("room_info.json", body, 0o644); err != nil {
		errf("写入 room_info.json 失败: %v", err)
		os.Exit(1)
	}
	logf("已保存 room_info.json (%d 字节)", len(body))
	logf("概要: %s", summarizeRoomInfo(body))
}

// summarizeRoomInfo 从 room/info 原始 JSON 提取重要字段摘要。
func summarizeRoomInfo(body []byte) string {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return truncate(string(body), 300)
	}
	var parts []string
	if v, ok := m["success"]; ok {
		parts = append(parts, "success="+fmt.Sprintf("%v", v))
	}
	data, _ := m["data"].(map[string]any)
	if data != nil {
		if sc, ok := data["seatConfig"].(map[string]any); ok {
			for _, k := range []string{"securityVerify", "captchaType", "reserveMode", "reserveAfterDay", "reserveAfterTime", "signDuration", "preSignDuration", "startTime", "endTime"} {
				if v, ok := sc[k]; ok {
					parts = append(parts, "seatConfig."+k+"="+fmt.Sprintf("%v", v))
				}
			}
		}
		if dt, ok := data["dynamicTimes"].([]any); ok && len(dt) > 0 {
			parts = append(parts, fmt.Sprintf("dynamicTimes[%d]首个=%v", len(dt), dt[0]))
		}
		if st, ok := data["screenTimes"].([]any); ok && len(st) > 0 {
			parts = append(parts, fmt.Sprintf("screenTimes[%d]首个=%v", len(st), st[0]))
		}
	}
	return strings.Join(parts, "  ")
}

// waitForCaptchaCode 轮询等待用户写入验证码文件（如 "1234"/"XCNC"）。
func waitForCaptchaCode(path string, timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			code := strings.TrimSpace(string(b))
			if code != "" {
				_ = os.Remove(path)
				return code, true
			}
		}
		time.Sleep(2 * time.Second)
	}
	return "", false
}

// cmdProbe 探测器：模拟页面完整流程（公告->占用查询->验证码->预约提交），
// 用于确认各接口可达性与服务器实际行为（测试专用）。
func cmdProbe(cfg *Config, start, end, captchaCode string, waitCaptcha bool) {
	sess, err := NewSession(cfg)
	if err != nil {
		errf("创建会话失败: %v", err)
		os.Exit(1)
	}
	if _, err := sess.Login(); err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	cp, err := sess.FetchCodePage()
	if err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	day := today(cfg)

	logf("[1] 确认公告弹窗...")
	if resp, err := sess.ConfirmDialog(); err != nil {
		warnf("dialog 接口异常: %v", err)
	} else {
		logf("dialog 响应: %s", truncate(resp, 300))
	}

	logf("[2] 查询座位占用 getusedtimes (seat=%s day=%s)...", padSeatNum(cfg.SeatNum), day)
	if resp, err := sess.GetUsedTimes(cfg.SeatNum, day); err != nil {
		warnf("getusedtimes 异常: %v", err)
	} else {
		logf("getusedtimes 响应: %s", truncate(resp, 500))
	}

	logf("[3] 查询未签退预约 check/exist...")
	if resp, err := sess.CheckExist(cfg.SeatNum); err != nil {
		warnf("check/exist 异常: %v", err)
	} else {
		logf("check/exist 响应: %s", truncate(resp, 300))
	}

	logf("[3.5] 验证码/风控配置...")
	if resp, err := sess.GetCaptchaType(); err != nil {
		warnf("captcha/type 异常: %v", err)
	} else {
		logf("captcha/type 响应: %s", resp)
	}
	if resp, err := sess.GetRiskConfig(); err != nil {
		warnf("risk/check/config 异常: %v", err)
	} else {
		logf("risk/check/config 响应: %s", resp)
	}

	logf("[4] 尝试获取验证码 captcha...")
	if u, err := sess.GetCaptcha(); err != nil {
		warnf("captcha 接口异常: %v", err)
	} else {
		if path, ok := saveCaptchaImage(u, "captcha_latest.png"); ok {
			logf("验证码图片已保存: %s (请在图片中识别验证码)", path)
			if waitCaptcha && captchaCode == "" {
				code, ok := waitForCaptchaCode("captcha_code.txt", 5*time.Minute)
				if !ok {
					errf("等待验证码输入超时，退出")
					os.Exit(1)
				}
				captchaCode = code
			}
		} else {
			logf("captcha 响应: %s", truncate(u, 300))
		}
	}

	logf("[5] 提交预约 %s %s~%s (验证码 '%s')...", day, start, end, captchaCode)
	if resp, err := sess.SubmitReserve(day, start, end, cfg.SeatNum, captchaCode, cp.SubmitEnc); err != nil {
		warnf("submit 异常: %v", err)
	} else {
		logf("submit 响应: %s", resp)
	}
}

// parseHHMM 解析 HH:MM。
func parseHHMM(s string) (time.Time, error) {
	return time.ParseInLocation("15:04", strings.TrimSpace(s), time.Local)
}

// ceilQuarter 向上取整到 15 分钟粒度。
func ceilQuarter(t time.Time) time.Time {
	r := ((t.Minute() + 14) / 15) * 15
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), r, 0, 0, t.Location())
}

// slotInfo 预约时段信息。
type slotInfo struct {
	Start time.Time
	End   time.Time
	Auto  bool // 是否自动选择
}

// resolveSlot 解析/自动选择预约时段。
// startParam/endParam 为 HH:MM，空则自动。autoSlot 开启时：
//   - 开始时间过期自动改为下一 15 分钟边界
//   - 结束时间不超过 start+duration，且不超过场地关闭时间
func resolveSlot(cfg *Config, startParam, endParam string, now time.Time) (slotInfo, error) {
	dur := time.Duration(cfg.ReserveDurationMinute) * time.Minute
	if dur <= 0 {
		dur = 240 * time.Minute
	}
	capEnd, err := parseHHMM(cfg.CapEnd)
	if err != nil {
		capEnd = time.Date(0, 1, 1, 22, 0, 0, 0, time.Local)
	}
	capTime := time.Date(now.Year(), now.Month(), now.Day(), capEnd.Hour(), capEnd.Minute(), 0, 0, now.Location())

	si := slotInfo{}
	if startParam == "" || cfg.AutoSlot {
		s, perr := parseHHMM(startParam)
		if perr == nil {
			si.Start = time.Date(now.Year(), now.Month(), now.Day(), s.Hour(), s.Minute(), 0, 0, now.Location())
			// 已过期的开始时间 -> 自动取下一 15 分钟边界
			if cfg.AutoSlot && si.Start.Add(3*time.Minute).Before(now) {
				si.Start = ceilQuarter(now.Add(2 * time.Minute))
				si.Auto = true
			}
		} else {
			si.Start = ceilQuarter(now.Add(2 * time.Minute))
			si.Auto = true
		}
	} else {
		s, perr := parseHHMM(startParam)
		if perr != nil {
			return si, fmt.Errorf("开始时间格式错误: %s", startParam)
		}
		si.Start = time.Date(now.Year(), now.Month(), now.Day(), s.Hour(), s.Minute(), 0, 0, now.Location())
	}

	if endParam == "" || cfg.AutoSlot {
		si.End = si.Start.Add(dur)
	} else {
		e, perr := parseHHMM(endParam)
		if perr != nil {
			return si, fmt.Errorf("结束时间格式错误: %s", endParam)
		}
		si.End = time.Date(now.Year(), now.Month(), now.Day(), e.Hour(), e.Minute(), 0, 0, now.Location())
	}

	if cfg.AutoSlot {
		if si.End.Sub(si.Start) > dur {
			logf("时段 %s~%s 超过单次上限 %d 分钟，自动调整为 %s", si.Start.Format("15:04"), si.End.Format("15:04"), int(dur.Minutes()), si.Start.Add(dur).Format("15:04"))
			si.End = si.Start.Add(dur)
		}
		if si.End.After(capTime) {
			logf("时段超过场地关闭 %s，截断为 %s", cfg.CapEnd, capTime.Format("15:04"))
			si.End = capTime
		}
		if si.End.Sub(si.Start) < time.Hour {
			return si, fmt.Errorf("今日可预约时段不足 1 小时 (剩余 %s)", si.End.Sub(si.Start).Round(time.Minute))
		}
	}
	return si, nil
}

// applyCapEnd 动态获取自习室关闭时间并写回配置（占座到闭馆）。
func applyCapEnd(cfg *Config) {
	sess, err := NewSession(cfg)
	if err != nil {
		warnf("获取关闭时间会话失败(使用配置值 %s): %v", cfg.CapEnd, err)
		return
	}
	if _, err := sess.Login(); err != nil {
		warnf("获取关闭时间登录失败(使用配置值 %s): %v", cfg.CapEnd, err)
		return
	}
	if e, err := sess.RoomCapEnd(); err == nil && e != "" {
		if e != cfg.CapEnd {
			logf("自习室关闭时间: %s (配置值 %s 将被覆盖，占座至闭馆)", e, cfg.CapEnd)
		} else {
			logf("自习室关闭时间: %s (与配置一致)", e)
		}
		cfg.CapEnd = e
	} else {
		warnf("解析关闭时间失败(使用配置值 %s): %v", cfg.CapEnd, err)
	}
}

// cmdBook 一次性抢座。
func cmdBook(cfg *Config, start, end string, seats []string, captchaToken string) {
	applyCapEnd(cfg)
	si, err := resolveSlot(cfg, start, end, time.Now())
	if err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	logf("开始抢座: 房间 %s 座位 %s 日期 %s 时间 %s~%s%s",
		cfg.RoomID, strings.Join(seatCandidates(cfg, seats), ","), today(cfg),
		si.Start.Format("15:04"), si.End.Format("15:04"), autoTag(si.Auto))
	if !doBookOnce(cfg, si.Start.Format("15:04"), si.End.Format("15:04"), seats, captchaToken, "", false) {
		errf("本轮未抢到座位")
		os.Exit(1)
	}
}

func autoTag(auto bool) string {
	if auto {
		return " (自动选择)"
	}
	return ""
}

// cmdRenew 续约：预约下一时段（与大厅"续约"同语义）。
// 自动规则：从当前使用中/待签到预约的结束时间开始预约下一时段；
// 若今日已到闭馆（剩余不足1小时），自动改为预约第二天。
func cmdRenew(cfg *Config) {
	applyCapEnd(cfg)

	sess, err := NewSession(cfg)
	if err != nil {
		errf("创建会话失败: %v", err)
		os.Exit(1)
	}
	if _, err := sess.Login(); err != nil {
		errf("%v", err)
		os.Exit(1)
	}
	list, lerr := sess.ListCurReserves()
	if lerr != nil {
		warnf("查询预约失败(忽略): %v", lerr)
	}

	dur := time.Duration(cfg.ReserveDurationMinute) * time.Minute
	if dur <= 0 {
		dur = 240 * time.Minute
	}
	now := time.Now()
	capEnd, perr := parseHHMM(cfg.CapEnd)
	if perr != nil {
		capEnd = time.Date(0, 1, 1, 22, 0, 0, 0, time.Local)
	}

	var start, end time.Time
	var lastEnd int64
	for _, r := range list {
		switch r.Status {
		case 0, 1, 3, 5, 9: // 待签到/使用中/暂离/被监督
			if r.EndTime >= lastEnd {
				lastEnd = r.EndTime
			}
		}
	}

	if lastEnd > 0 {
		start = time.UnixMilli(lastEnd)
		if start.Before(now) {
			start = ceilQuarter(now.Add(2 * time.Minute))
		}
		end = start.Add(dur)
		capTime := time.Date(start.Year(), start.Month(), start.Day(), capEnd.Hour(), capEnd.Minute(), 0, 0, start.Location())
		if end.After(capTime) {
			end = capTime
		}
		if end.Sub(start) < time.Hour {
			// 今日已占至闭馆 -> 预约明天
			tomorrow := start.AddDate(0, 0, 1)
			startStr := cfg.StartTime
			if startStr == "" {
				startStr = "08:00"
			}
			s, serr := parseHHMM(startStr)
			if serr != nil {
				errf("start_time 格式错误: %v", serr)
				os.Exit(1)
			}
			start = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), s.Hour(), s.Minute(), 0, 0, tomorrow.Location())
			end = start.Add(dur)
			capTime = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), capEnd.Hour(), capEnd.Minute(), 0, 0, tomorrow.Location())
			if end.After(capTime) {
				end = capTime
			}
			logf("今日已占至闭馆 %s，自动改为预约明日 %s~%s", cfg.CapEnd, start.Format("15:04"), end.Format("15:04"))
		} else {
			logf("上一预约于 %s 结束，续约下一时段 %s~%s", start.Format("15:04"), start.Format("15:04"), end.Format("15:04"))
		}
	} else {
		start = ceilQuarter(now.Add(2 * time.Minute))
		end = start.Add(dur)
		capTime := time.Date(start.Year(), start.Month(), start.Day(), capEnd.Hour(), capEnd.Minute(), 0, 0, start.Location())
		if end.After(capTime) {
			end = capTime
		}
		if end.Sub(start) < time.Hour {
			errf("今日剩余可预约时段不足 1 小时，无可用续约时段")
			os.Exit(1)
		}
		logf("无进行中的预约，直接预约 %s~%s", start.Format("15:04"), end.Format("15:04"))
	}

	logf("开始续约: 座位 %s 时间 %s~%s", cfg.SeatNum, start.Format("15:04"), end.Format("15:04"))
	if !doBookOnce(cfg, start.Format("15:04"), end.Format("15:04"), nil, cfg.Captcha, start.Format("2006-01-02"), false) {
		errf("续约失败")
		os.Exit(1)
	}
	logf("===== 续约成功: %s~%s =====", start.Format("15:04"), end.Format("15:04"))
	if cfg.AutoSign {
		logf("提示: 该预约将于 %s 开启签到窗口 (自动签到用 watch 或 sign 命令)", start.Add(-time.Duration(cfg.SignAheadMinute)*time.Minute).Format("15:04"))
	}
}

// cmdWatch 全自动守卫模式：连续占座（今日→明日→……每天续约到闭馆，自动签到）。
func cmdWatch(cfg *Config) {
	grabAt, err := time.ParseInLocation("15:04:05", cfg.GrabAt, time.Local)
	if err != nil {
		errf("grab_at 格式错误，应为 HH:MM:SS: %v", err)
		os.Exit(1)
	}
	applyCapEnd(cfg)
	now := time.Now()

	// 检查是否已有有效预约（今日/未来）；有则直接进入签到+续约循环
	var curEnd time.Time
	if probeSess, perr := NewSession(cfg); perr == nil {
		if _, lerr := probeSess.Login(); lerr == nil {
			if list, lerr := probeSess.ListCurReserves(); lerr == nil {
				for _, r := range list {
					switch r.Status {
					case 0, 1, 3, 5, 9:
						e := time.UnixMilli(r.EndTime)
						if e.After(curEnd) {
							curEnd = e
						}
					}
				}
			}
		}
	}

	if curEnd.IsZero() {
		// 无任何预约：等待抢座时间（或已过则立即）
		grab := time.Date(now.Year(), now.Month(), now.Day(), grabAt.Hour(), grabAt.Minute(), grabAt.Second(), 0, time.Local)
		if now.Before(grab) {
			wait := time.Until(grab)
			logf("当前 %s，距离抢座时间 %s 还有 %s", now.Format("15:04:05"), cfg.GrabAt, formatDuration(int(wait.Seconds())))
			time.Sleep(wait)
		}
		logf("到达抢座时间，开始抢座...")
		si, err := resolveSlot(cfg, cfg.StartTime, cfg.EndTime, time.Now())
		if err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		if !doBookOnce(cfg, si.Start.Format("15:04"), si.End.Format("15:04"), nil, cfg.Captcha, "", false) {
			errf("抢座失败（可能是时段冲突/已被预约），请稍后重试")
			os.Exit(1)
		}
		logf("========== 首轮预约成功: %s~%s ==========", si.Start.Format("15:04"), si.End.Format("15:04"))
		curEnd = si.End
	} else {
		logf("检测到已有预约(至 %s)，跳过抢座，直接进入签到/续约流程", curEnd.Format("15:04"))
	}

	// 自动签到（等待签到窗口开启）
	if cfg.AutoSign {
		signSess, serr := NewSession(cfg)
		if serr != nil {
			warnf("自动签到会话创建失败: %v", serr)
		} else if _, lerr := signSess.Login(); lerr != nil {
			warnf("自动签到登录失败: %v", lerr)
		} else {
			go ensureSign(cfg, signSess)
		}
	}

	// 续约循环
	renewAhead := time.Duration(cfg.RenewAheadMinute) * time.Minute
	if renewAhead <= 0 {
		renewAhead = 10 * time.Minute
	}
	for {
		dur := time.Duration(cfg.ReserveDurationMinute) * time.Minute
		if dur <= 0 {
			dur = 240 * time.Minute
		}
		capEnd, perr := parseHHMM(cfg.CapEnd)
		if perr != nil {
			capEnd = time.Date(0, 1, 1, 22, 0, 0, 0, time.Local)
		}
		capTime := time.Date(curEnd.Year(), curEnd.Month(), curEnd.Day(), capEnd.Hour(), capEnd.Minute(), 0, 0, curEnd.Location())

		nextStart := curEnd
		now = time.Now()
		if nextStart.Before(now) {
			// 断了续约（进程暂停/失败过），从当前时间继续
			nextStart = ceilQuarter(now.Add(2 * time.Minute))
			logf("上一时段已结束，从 %s 继续预约", nextStart.Format("15:04"))
		}
		nextEnd := nextStart.Add(dur)
		if nextEnd.After(capTime) {
			nextEnd = capTime
		}
		if nextEnd.Sub(nextStart) < time.Hour {
			// 今日链条已到闭馆：立即预约明天第一段（当晚 19:00 起窗口已开），继续循环
			logf("今日已占至闭馆 %s，预约明日第一段...", cfg.CapEnd)
			tomorrow := nextStart.AddDate(0, 0, 1)
			startStr := cfg.StartTime
			if startStr == "" {
				startStr = "08:00"
			}
			s, serr := parseHHMM(startStr)
			if serr != nil {
				errf("start_time 格式错误: %v", serr)
				os.Exit(1)
			}
			tStart := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), s.Hour(), s.Minute(), 0, 0, tomorrow.Location())
			tEnd := tStart.Add(dur)
			if tEnd.After(time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), capEnd.Hour(), capEnd.Minute(), 0, 0, tomorrow.Location())) {
				tEnd = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), capEnd.Hour(), capEnd.Minute(), 0, 0, tomorrow.Location())
			}
			// 若已有明日预约（如提前续约的），直接用其结束时间
			existingEnd, found, _ := latestReserveEnd(cfg)
			if found && existingEnd.After(curEnd) {
				logf("检测到已有明日预约 (至 %s)，跳过重复预约", existingEnd.Format("15:04"))
				curEnd = existingEnd
				continue
			}
			logf("预约明日第一段 %s~%s ...", tStart.Format("15:04"), tEnd.Format("15:04"))
			if !doBookOnce(cfg, tStart.Format("15:04"), tEnd.Format("15:04"), nil, cfg.Captcha, tStart.Format("2006-01-02"), true) {
				warnf("明日抢座失败，%s 后重试", renewAhead)
				time.Sleep(renewAhead)
				continue
			}
			curEnd = tEnd
			logf("已预约明日 %s~%s，自动签到将在窗口期执行", tStart.Format("15:04"), tEnd.Format("15:04"))
			if cfg.AutoSign {
				signSess, serr := NewSession(cfg)
				if serr != nil {
					warnf("自动签到会话创建失败: %v", serr)
				} else if _, lerr := signSess.Login(); lerr != nil {
					warnf("自动签到登录失败: %v", lerr)
				} else {
					go ensureSign(cfg, signSess)
				}
			}
			continue
		}

		target := nextStart.Add(-renewAhead)
		if now.Before(target) {
			logf("当前时段 %s 结束，%s 后自动预约下一时段 %s~%s", curEnd.Format("15:04"), formatDuration(int(time.Until(target).Seconds())), nextStart.Format("15:04"), nextEnd.Format("15:04"))
			time.Sleep(time.Until(target))
		}
		logf("执行续约: %s~%s ...", nextStart.Format("15:04"), nextEnd.Format("15:04"))
		if doBookOnce(cfg, nextStart.Format("15:04"), nextEnd.Format("15:04"), nil, cfg.Captcha, "", true) {
			curEnd = nextEnd
			logf("续约成功，当前预约至 %s", curEnd.Format("15:04"))
			// 新预约需要再次签到
			if cfg.AutoSign {
				signSess, serr := NewSession(cfg)
				if serr != nil {
					warnf("自动签到会话创建失败: %v", serr)
				} else if _, lerr := signSess.Login(); lerr != nil {
					warnf("自动签到登录失败: %v", lerr)
				} else {
					go ensureSign(cfg, signSess)
				}
			}
		} else {
			warnf("续约失败，%s 后重试", renewAhead)
			time.Sleep(renewAhead)
		}
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `自习室自动占座系统 (超星 seatengine) - 全自动版

用法:
  chaoxing-book <命令> [-c config.json]

常用命令:
  check     登录并检查座位页/房间信息（不会预约）
  captcha   独立测试滑块验证码求解（成功写入 validate_token.txt）
  book      立即抢座（自动解滑块验证码）
  sign      自动签到（可 -qr 识别桌上二维码照片；自动找待签到预约）
  renew     立即续约一次
  watch     守护模式：到点抢座→自动签到→按间隔自动续约+续签到
  cancel    取消预约 (需要 -captcha <reserveId>)

调试命令:
  probe     全链路探测（公告/占用/验证码/提交测试）
  info      房间信息保存 room_info.json
  submitx   提交变体测试 (-captcha <0|1|2>)
  index     抓取登录后首页

全局参数:
  -c string       配置文件路径 (默认 "config.json")
  -start HH:MM    覆盖开始时间 (book/probe)
  -end HH:MM      覆盖结束时间 (book/probe)
  -seat 117,118   覆盖候选座位 (book)
  -captcha xxx    validate token / reserveId (sign、cancel)
  -qr 图片路径     座位二维码照片 (sign 自动识别校验)

示例:
  chaoxing-book check -c config.json
  chaoxing-book book -c config.json
  chaoxing-book sign -c config.json -qr 二维码.jpg
  chaoxing-book watch -c config.json
`)
}
