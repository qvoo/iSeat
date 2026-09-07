package main

import (
	"fmt"
	"log"
	"os"
	"time"
)

var (
	// logger 统一日志输出，带时间戳。
	logger = log.New(os.Stdout, "", log.LstdFlags)
)

// logf 普通信息日志。
func logf(format string, args ...any) {
	logger.Printf("[信息] "+format, args...)
}

// warnf 警告日志。
func warnf(format string, args ...any) {
	logger.Printf("[警告] "+format, args...)
}

// errf 错误日志。
func errf(format string, args ...any) {
	logger.Printf("[错误] "+format, args...)
}

// sleepMS 按毫秒休眠（带轻微随机抖动，避免请求过于规律）。
func sleepMS(ms int) {
	if ms <= 0 {
		return
	}
	jitter := time.Duration(ms/10) * time.Millisecond
	base := time.Duration(ms) * time.Millisecond
	if jitter > 0 {
		time.Sleep(base + time.Duration(randInt(0, int(jitter/time.Millisecond)))*time.Millisecond)
	} else {
		time.Sleep(base)
	}
}

func randInt(min, max int) int {
	if max <= min {
		return min
	}
	// 简单伪随机，避免引入外部依赖
	return min + int(time.Now().UnixNano()%int64(max-min))
}

// formatDuration 把秒数格式化为人类可读。
func formatDuration(sec int) string {
	if sec < 60 {
		return fmt.Sprintf("%d秒", sec)
	}
	if sec < 3600 {
		return fmt.Sprintf("%d分", sec/60)
	}
	return fmt.Sprintf("%d小时%d分", sec/3600, sec%3600/60)
}
