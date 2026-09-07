package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// loginAESKey 超星登录口令加密密钥(固定)，同时作为 IV 使用。
const loginAESKey = "u2oh6Vu^HWe4_AES"

// AesCbcPkcs7Base64 使用 AES-128-CBC + PKCS7 填充 + Base64 加密明文。
// 超星 fanyalogin 对账号/密码采用该方案。
func AesCbcPkcs7Base64(plain string) (string, error) {
	key := []byte(loginAESKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes new cipher: %w", err)
	}

	padded := pkcs7Pad([]byte(plain), aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	// IV 与 key 相同，16 字节
	mode := cipher.NewCBCEncrypter(block, key)
	mode.CryptBlocks(ciphertext, padded)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// pkcs7Pad 按 PKCS7 规则补位，块长必须 <= 255。
func pkcs7Pad(data []byte, blockSize int) []byte {
	padLen := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(padLen)}, padLen)...)
}

// BuildEnc 计算预约接口的 enc 签名参数。
// 拼接串为固定格式：
//
//	[captcha=][day=2026-09-01][endTime=22:00][roomId=5703][seatNum=117][startTime=08:00][token=xxx][%sd`~7^/>N4!Q#){'']
//
// 截取 MD5 hex。注意这里必须用字符串拼接而非 fmt.Sprintf，
// 因为 %sd 中的 % 若作为格式化占位符会被吞掉。
func BuildEnc(day, endTime, roomID, seatNum, startTime, token string) string {
	content := "[captcha=][day=" + day + "][endTime=" + endTime +
		"][roomId=" + roomID + "][seatNum=" + seatNum +
		"][startTime=" + startTime + "][token=" + token +
		"][%sd`~7^/>N4!Q#){'']"
	sum := md5.Sum([]byte(content))
	return hex.EncodeToString(sum[:])
}

// BuildEncNew 新版 enc：参数对象经 Object.keys().sort() 后按字母序拼接
// [key=value]，再追加页面 submit_enc 的 [value]，最后取 MD5 hex。
// 对应 submitVerify.min.js verifyParam 的 case 序列 (3|1|0|2|6|4|5)。
func BuildEncNew(params [][2]string, submitEnc string) string {
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
	return md5Hex(sb.String())
}

// md5Hex 通用 md5 hex 输出（用于调试/确认）。
func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
