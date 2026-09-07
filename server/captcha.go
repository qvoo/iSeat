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

const (
	cxCaptchaID    = "42sxgHoTPTKbt0uZxPJ7ssOvtXr3ZgZ1"
	cxType         = "slide"
	cxVersion      = "1.1.20"
	cxRunEnv       = 10
	cxBase         = "https://captcha.chaoxing.com"
	cxCallback     = "cx_captcha_function"
	cxMaxSlideX    = 264
	cxOffsetsInit  = "0,-6,6,-11,11,-16,16"
)

var cxJSONPRe = regexp.MustCompile(`^[^(]*\((.*)\)\s*;?\s*$`)
var cxUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// CaptchaSolver 滑块验证码求解器。
type CaptchaSolver struct {
	client *http.Client
}

// NewCaptchaSolver 创建求解器。
func NewCaptchaSolver() *CaptchaSolver {
	return &CaptchaSolver{client: &http.Client{Timeout: 25 * time.Second}}
}

func (s *CaptchaSolver) get(u string, referer string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", cxUA)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (s *CaptchaSolver) jsonp(u string) (map[string]any, error) {
	b, err := s.get(u, "")
	if err != nil {
		return nil, err
	}
	txt := string(b)
	if m := cxJSONPRe.FindStringSubmatch(txt); m != nil {
		txt = m[1]
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(txt), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func cxMd5(x string) string {
	h := md5.Sum([]byte(x))
	return hex.EncodeToString(h[:])
}

func cxUuid() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(rand.Intn(256))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Solve 解滑块验证码返回 validate token。
func (s *CaptchaSolver) Solve(referer string, maxAttempt int) (string, error) {
	offsets := []int{}
	for _, v := range strings.Split(cxOffsetsInit, ",") {
		n, _ := strconv.Atoi(v)
		offsets = append(offsets, n)
	}
	if maxAttempt <= 0 || maxAttempt > len(offsets) {
		maxAttempt = len(offsets)
	}
	for i := 0; i < maxAttempt; i++ {
		token, err := s.solveOnce(referer, offsets[i])
		if err != nil {
			return "", err
		}
		if token != "" {
			return token, nil
		}
	}
	return "", fmt.Errorf("滑块验证失败(%d次)", maxAttempt)
}

func (s *CaptchaSolver) solveOnce(referer string, offset int) (string, error) {
	conf, err := s.jsonp(fmt.Sprintf("%s/captcha/get/conf?captchaId=%s&callback=%s", cxBase, cxCaptchaID, cxCallback))
	if err != nil {
		return "", err
	}
	tv, _ := conf["t"].(float64)
	serverTime := int64(tv)
	captchaKey := cxMd5(strconv.FormatInt(serverTime, 10) + cxUuid())
	token := cxMd5(strconv.FormatInt(serverTime, 10)+cxCaptchaID+cxType+captchaKey) + ":" + strconv.FormatInt(serverTime+300000, 10)
	iv := cxMd5(cxCaptchaID + cxType + strconv.FormatInt(time.Now().UnixMilli(), 10) + cxUuid())
	imgURL := fmt.Sprintf("%s/captcha/get/verification/image?captchaId=%s&type=%s&version=%s&captchaKey=%s&token=%s&referer=%s&iv=%s&callback=%s",
		cxBase, cxCaptchaID, cxType, cxVersion, captchaKey,
		url.QueryEscape(token), url.QueryEscape(referer), iv, cxCallback)
	imgResp, err := s.jsonp(imgURL)
	if err != nil {
		return "", err
	}
	imgToken, _ := imgResp["token"].(string)
	vo, _ := imgResp["imageVerificationVo"].(map[string]any)
	if vo == nil {
		return "", fmt.Errorf("验证图响应缺少 imageVerificationVo")
	}
	shadeURL, _ := vo["shadeImage"].(string)
	cutURL, _ := vo["cutoutImage"].(string)
	shade, err := s.get(shadeURL, referer)
	if err != nil {
		return "", err
	}
	cut, err := s.get(cutURL, referer)
	if err != nil {
		return "", err
	}
	bestX, err := matchGapX(shade, cut)
	if err != nil {
		return "", err
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
	ck, err := s.jsonp(checkURL)
	if err != nil {
		return "", err
	}
	if r, ok := ck["result"].(bool); ok && r {
		extra, _ := ck["extraData"].(string)
		var ed map[string]any
		if json.Unmarshal([]byte(extra), &ed) == nil {
			if v, ok := ed["validate"].(string); ok && v != "" {
				return v, nil
			}
		}
	}
	return "", nil
}

// matchGapX 归一化互相关找缺口。
func matchGapX(shade, cut []byte) (int, error) {
	bgImg, _, err := image.Decode(bytes.NewReader(shade))
	if err != nil {
		return 0, err
	}
	pieceImg, _, err := image.Decode(bytes.NewReader(cut))
	if err != nil {
		return 0, err
	}
	bw, bh := bgImg.Bounds().Dx(), bgImg.Bounds().Dy()
	pw, ph := pieceImg.Bounds().Dx(), pieceImg.Bounds().Dy()
	if bw < pw || bh < ph {
		return 0, fmt.Errorf("拼图块大于背景")
	}
	type pxT struct {
		x, y int
		v    float64
	}
	var mask []pxT
	var pv []float64
	sumP := 0.0
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			r, g, b, a := pieceImg.At(x, y).RGBA()
			if int(a>>8) < 100 {
				continue
			}
			v := (float64(r>>8) + float64(g>>8) + float64(b>>8)) / 3
			mask = append(mask, pxT{x, y, v})
			pv = append(pv, v)
			sumP += v
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
		return 0, fmt.Errorf("拼图块无纹理")
	}
	bgGray := make([]float64, bw*bh)
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			r, g, b, _ := bgImg.At(x, y).RGBA()
			bgGray[y*bw+x] = (float64(r>>8) + float64(g>>8) + float64(b>>8)) / 3
		}
	}
	bestX, bestScore := 0, math.Inf(-1)
	for x0 := 0; x0 <= bw-pw; x0++ {
		bSum, denomB, corr := 0.0, 0.0, 0.0
		for _, m := range mask {
			bSum += bgGray[m.y*bw+(x0+m.x)]
		}
		meanB := bSum / float64(n)
		for _, m := range mask {
			d := bgGray[m.y*bw+(x0+m.x)] - meanB
			denomB += d * d
			corr += (m.v - meanP) * d
		}
		denomB = math.Sqrt(denomB)
		if denomB < 1e-9 {
			continue
		}
		sc := corr / (denomP * denomB)
		if sc > bestScore {
			bestScore = sc
			bestX = x0
		}
	}
	return bestX, nil
}
