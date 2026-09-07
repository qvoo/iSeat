package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

// TestBuildEncKnownVector 使用吾爱破解公开文章中的已知样例校验 enc 算法：
// day=2023-06-03 endTime=19:00 roomId=1901 seatNum=022 startTime=18:30
// token=818a0f43a1a64bc98acb3f572fb64336 -> enc=4e66e0f4a1f3db313ad08288b9c379da
func TestBuildEncKnownVector(t *testing.T) {
	got := BuildEnc("2023-06-03", "19:00", "1901", "022", "18:30", "818a0f43a1a64bc98acb3f572fb64336")
	want := "4e66e0f4a1f3db313ad08288b9c379da"
	if got != want {
		t.Fatalf("enc mismatch: got %s want %s", got, want)
	}
}

// TestAesRoundTrip 验证 AES-128-CBC + PKCS7 + Base64 加解密自洽。
func TestAesRoundTrip(t *testing.T) {
	plain := "testuser12345678" // 仅用于算法自洽性测试的占位数据
	enc, err := AesCbcPkcs7Base64(plain)
	if err != nil {
		t.Fatal(err)
	}
	key := []byte(loginAESKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	if len(ct)%aes.BlockSize != 0 {
		t.Fatalf("ciphertext length %d not multiple of block size", len(ct))
	}
	mode := cipher.NewCBCDecrypter(block, key)
	pt := make([]byte, len(ct))
	mode.CryptBlocks(pt, ct)
	padLen := int(pt[len(pt)-1])
	if padLen <= 0 || padLen > aes.BlockSize {
		t.Fatalf("invalid padding length %d", padLen)
	}
	pt = pt[:len(pt)-padLen]
	if !bytes.Equal(pt, []byte(plain)) {
		t.Fatalf("roundtrip failed: got %q", string(pt))
	}
}

// TestBuildEncNewSortedOrder 验证 enc 参数按字母序拼接（Object.keys().sort()）。
func TestBuildEncNewSortedOrder(t *testing.T) {
	params := [][2]string{
		{"roomId", "5703"},
		{"day", "2026-09-07"},
		{"startTime", "19:30"},
		{"endTime", "20:30"},
		{"seatNum", "117"},
		{"captcha", ""},
		{"type", "1"},
		{"verifyData", "1"},
		{"wyToken", ""},
	}
	want := "[captcha=][day=2026-09-07][endTime=20:30][roomId=5703][seatNum=117][startTime=19:30][type=1][verifyData=1][wyToken=][abc123]"
	sum := md5.Sum([]byte(want))
	wantEnc := hex.EncodeToString(sum[:])
	if got := BuildEncNew(params, "abc123"); got != wantEnc {
		t.Fatalf("BuildEncNew mismatch:\n got %s\nwant %s", got, wantEnc)
	}
}

// TestPadSeatNum 验证座位号自动补零。
func TestPadSeatNum(t *testing.T) {
	cases := map[string]string{
		"7":   "007",
		"47":  "047",
		"117": "117",
		" 8 ": "008",
	}
	for in, want := range cases {
		if got := padSeatNum(in); got != want {
			t.Errorf("padSeatNum(%q) = %q, want %q", in, got, want)
		}
	}
}
