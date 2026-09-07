package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 超星 CX 滑块验证码协议参数（逆向自 captcha.chaoxing.com/load-d.min.js）
const (
	cxCaptchaID   = "42sxgHoTPTKbt0uZxPJ7ssOvtXr3ZgZ1" // 座位业务 captchaId
	cxType        = "slide"
	cxVersion     = "1.1.20"
	cxRunEnv      = 10
	cxBase        = "https://captcha.chaoxing.com"
	cxCallback    = "cx_captcha_function"
	cxMaxSlideX   = 264 // 320 - 56
	cxOffsetsInit = "0,-6,6,-11,11,-16,16"
)

// CXSolveResult 验证码求解结果。
type CXSolveResult struct {
	Validate string
	Token    string
	BestX    int
}

var cxJSONPRe = regexp.MustCompile(`^[^(]*\((.*)\)\s*;?\s*$`)

func cxMD5(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func cxUUID() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(rand.Intn(256))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// CXCaptcha 求解器（与 seat 会话无关的验证码后端）。
type CXCaptcha struct {
	client  *http.Client
	referer string
}

// NewCXCaptcha 创建求解器。
func NewCXCaptcha(referer string) *CXCaptcha {
	return &CXCaptcha{client: &http.Client{Timeout: 25 * time.Second}, referer: referer}
}

func (c *CXCaptcha) get(urlAddr string, referer bool) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, urlAddr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", uaForCaptcha())
	if referer {
		req.Header.Set("Referer", c.referer)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func uaForCaptcha() string {
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
}

func (c *CXCaptcha) jsonpGet(urlAddr string) (map[string]any, error) {
	body, err := c.get(urlAddr, true)
	if err != nil {
		return nil, err
	}
	text := string(body)
	var m map[string]any
	if match := cxJSONPRe.FindStringSubmatch(text); match != nil {
		text = match[1]
	}
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		return nil, fmt.Errorf("jsonp 解析失败: %v (原文: %s)", err, truncate(text, 200))
	}
	return m, nil
}

// Solve 完整求解滑块验证码，返回 validate token。
// 每次失败会重新获取谜题（token 一次性），因此按候选偏移依次尝试。
func (c *CXCaptcha) Solve(maxAttempt int) (*CXSolveResult, error) {
	var offsets []int
	for _, s := range strings.Split(cxOffsetsInit, ",") {
		v, _ := strconv.Atoi(s)
		offsets = append(offsets, v)
	}
	if maxAttempt <= 0 || maxAttempt > len(offsets) {
		maxAttempt = len(offsets)
	}

	for i := 0; i < maxAttempt; i++ {
		res, code, err := c.solveOnce(offsets[i])
		if err != nil {
			logf("captcha 尝试%d 出错: %v", i+1, err)
			continue
		}
		if code == "" {
			return res, nil
		}
		logf("captcha 尝试%d (x=%d%s) 未通过: %s", i+1, res.BestX, offsStr(offsets[i]), code)
	}
	return nil, fmt.Errorf("滑块验证失败（尝试 %d 次）", maxAttempt)
}

func offsStr(o int) string {
	if o >= 0 {
		return "+" + strconv.Itoa(o)
	}
	return strconv.Itoa(o)
}

// solveOnce 执行一轮：conf -> image -> 检测缺口 -> check。
func (c *CXCaptcha) solveOnce(offset int) (*CXSolveResult, string, error) {
	conf, err := c.jsonpGet(fmt.Sprintf("%s/captcha/get/conf?captchaId=%s&callback=%s", cxBase, cxCaptchaID, cxCallback))
	if err != nil {
		return nil, "", err
	}
	tv, _ := conf["t"].(float64)
	serverTime := int64(tv)

	captchaKey := cxMD5(strconv.FormatInt(serverTime, 10) + cxUUID())
	token := cxMD5(strconv.FormatInt(serverTime, 10)+cxCaptchaID+cxType+captchaKey) + ":" + strconv.FormatInt(serverTime+300000, 10)
	iv := cxMD5(cxCaptchaID + cxType + strconv.FormatInt(time.Now().UnixMilli(), 10) + cxUUID())

	imgURL := fmt.Sprintf("%s/captcha/get/verification/image?captchaId=%s&type=%s&version=%s&captchaKey=%s&token=%s&referer=%s&iv=%s&callback=%s",
		cxBase, cxCaptchaID, cxType, cxVersion, captchaKey,
		url.QueryEscape(token), url.QueryEscape(c.referer), iv, cxCallback)
	imgResp, err := c.jsonpGet(imgURL)
	if err != nil {
		return nil, "", err
	}
	imgToken, _ := imgResp["token"].(string)
	vo, _ := imgResp["imageVerificationVo"].(map[string]any)
	if vo == nil {
		return nil, "", fmt.Errorf("image 响应缺少 imageVerificationVo")
	}
	shadeURL, _ := vo["shadeImage"].(string)
	cutURL, _ := vo["cutoutImage"].(string)
	if shadeURL == "" || cutURL == "" {
		return nil, "", fmt.Errorf("image 响应缺少图片 URL")
	}

	shade, err := c.get(shadeURL, true)
	if err != nil {
		return nil, "", err
	}
	cut, err := c.get(cutURL, true)
	if err != nil {
		return nil, "", err
	}
	bestX, err := matchGapX(shade, cut)
	if err != nil {
		return nil, "", err
	}
	bestX += offset
	if bestX < 0 {
		bestX = 0
	}
	if bestX > cxMaxSlideX {
		bestX = cxMaxSlideX
	}

	clickArr := fmt.Sprintf(`[{"x":%d}]`, bestX)
	checkURL := fmt.Sprintf("%s/captcha/check/verification/result?captchaId=%s&type=%s&token=%s&textClickArr=%s&coordinate=%s&runEnv=%d&version=%s&t=c&iv=%s&callback=%s",
		cxBase, cxCaptchaID, cxType, imgToken,
		url.QueryEscape(clickArr), url.QueryEscape("[]"),
		cxRunEnv, cxVersion, iv, cxCallback)
	checkResp, err := c.jsonpGet(checkURL)
	if err != nil {
		return nil, "", err
	}
	res := &CXSolveResult{Token: imgToken, BestX: bestX}
	if r, ok := checkResp["result"].(bool); ok && r {
		extra, _ := checkResp["extraData"].(string)
		var ed map[string]any
		if json.Unmarshal([]byte(extra), &ed) == nil {
			if v, ok := ed["validate"].(string); ok && v != "" {
				res.Validate = v
				return res, "", nil
			}
		}
		return res, "", fmt.Errorf("check 通过但 extraData 无 validate: %s", truncate(extra, 200))
	}
	msg, _ := checkResp["msg"].(string)
	return res, fmt.Sprintf("result=false (%s)", msg), nil
}

// matchGapX 模板匹配：将拼图块(含alpha)与背景图做归一化互相关，返回最佳 x。
func matchGapX(shade, cut []byte) (int, error) {
	bgImg, _, err := image.Decode(bytes.NewReader(shade))
	if err != nil {
		return 0, fmt.Errorf("背景图解码失败: %w", err)
	}
	pieceImg, _, err := image.Decode(bytes.NewReader(cut))
	if err != nil {
		return 0, fmt.Errorf("拼图块解码失败: %w", err)
	}
	bw, bh := bgImg.Bounds().Dx(), bgImg.Bounds().Dy()
	pw, ph := pieceImg.Bounds().Dx(), pieceImg.Bounds().Dy()
	if bw < pw || bh < ph {
		return 0, fmt.Errorf("拼图块(%dx%d)大于背景(%dx%d)", pw, ph, bw, bh)
	}
	bx, by := bgImg.Bounds().Min.X, bgImg.Bounds().Min.Y
	px0, py0 := pieceImg.Bounds().Min.X, pieceImg.Bounds().Min.Y

	// 拼图掩码与灰度
	type pxT struct{ x, y int; v float64 }
	var mask []pxT
	var pv []float64
	sumP, sumP2 := 0.0, 0.0
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			r, g, b, a := pieceImg.At(px0+x, py0+y).RGBA()
			if int(a>>8) < 100 {
				continue
			}
			v := (float64(r>>8) + float64(g>>8) + float64(b>>8)) / 3
			mask = append(mask, pxT{x, y, v})
			pv = append(pv, v)
			sumP += v
			sumP2 += v * v
		}
	}
	n := len(pv)
	if n == 0 {
		return 0, fmt.Errorf("拼图块无有效像素")
	}
	meanP := sumP / float64(n)
	var denomP float64
	for _, v := range pv {
		d := v - meanP
		denomP += d * d
	}
	denomP = math.Sqrt(denomP)
	if denomP < 1e-9 {
		return 0, fmt.Errorf("拼图块无有效纹理")
	}

	// 背景灰度
	bgGray := make([]float64, bw*bh)
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			r, g, b, _ := bgImg.At(bx+x, by+y).RGBA()
			bgGray[y*bw+x] = (float64(r>>8) + float64(g>>8) + float64(b>>8)) / 3
		}
	}

	bestX, bestScore := 0, math.Inf(-1)
	for x0 := 0; x0 <= bw-pw; x0++ {
		var bSum, denomB, xCorr float64
		for _, m := range mask {
			bv := bgGray[m.y*bw + (x0 + m.x)]
			bSum += bv
		}
		meanB := bSum / float64(n)
		for _, m := range mask {
			bv := bgGray[m.y*bw + (x0 + m.x)]
			d := bv - meanB
			denomB += d * d
			xCorr += (m.v - meanP) * d
		}
		denomB = math.Sqrt(denomB)
		if denomB < 1e-9 {
			continue
		}
		score := xCorr / (denomP * denomB)
		if score > bestScore {
			bestScore = score
			bestX = x0
		}
	}
	logf("captcha 缺口检测: bestX=%d score=%.4f", bestX, bestScore)
	return bestX, nil
}
