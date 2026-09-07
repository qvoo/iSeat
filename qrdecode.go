package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// DecodeQRFile 解码图片中的二维码，返回文本内容。
func DecodeQRFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return "", fmt.Errorf("图片解码失败: %w", err)
	}
	return DecodeQRImage(img)
}

// DecodeQRImage 从图像中解码二维码。
func DecodeQRImage(img image.Image) (string, error) {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", err
	}
	reader := qrcode.NewQRCodeReader()
	res, err := reader.Decode(bmp, nil)
	if err != nil {
		return "", err
	}
	return res.GetText(), nil
}
