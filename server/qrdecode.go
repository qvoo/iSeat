package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"strconv"
	"strings"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// QRInfo 解析出的座位二维码信息。
type QRInfo struct {
	RoomID  string
	SeatNum string
	SeatID  string
	Raw     string
}

// DecodeQR 从图片字节解码桌面二维码，并解析出座位参数。
func DecodeQR(data []byte) (*QRInfo, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("图片解码失败: %w", err)
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return nil, err
	}
	res, err := qrcode.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		return nil, fmt.Errorf("二维码识别失败: %w (请拍摄清晰正面照片)", err)
	}
	text := strings.TrimSpace(res.GetText())
	info := &QRInfo{Raw: text}
	q, err := url.Parse(text)
	if err == nil && q.Host != "" {
		query := q.Query()
		info.RoomID = query.Get("id")
		info.SeatNum = query.Get("seatNum")
		info.SeatID = query.Get("seatId")
		if info.SeatID == "" {
			info.SeatID = "105" // 兜底: 本校区业务 seatId
		}
	}
	if info.RoomID == "" || info.SeatNum == "" {
		return nil, fmt.Errorf("二维码内容不完整: %s", text)
	}
	// 兼容 8位/纯数字/带引号等异常
	if _, err := strconv.Atoi(info.RoomID); err != nil {
		return nil, fmt.Errorf("roomId 异常: %s", info.RoomID)
	}
	return info, nil
}
