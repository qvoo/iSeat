package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]

	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	configPath := fs.String("c", "config.json", "配置文件路径")
	// book 命令可用参数
	start := fs.String("start", "", "开始时间 HH:MM (覆盖配置)")
	end := fs.String("end", "", "结束时间 HH:MM (覆盖配置)")
	seats := fs.String("seat", "", "座位号，多个用逗号分隔 (覆盖配置, 按优先级尝试)")
	captchaCode := fs.String("captcha", "", "验证码/reserveId (probe/sign/cancel 专用)")
	qrPath := fs.String("qr", "", "座位二维码图片路径 (sign 命令自动识别)")
	waitCaptcha := fs.Bool("wait-captcha", false, "probe: 保存验证码后等待 captcha_code.txt 文件被写入再提交")

	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		errf("%v", err)
		os.Exit(1)
	}

	if cfg.Debug {
		logf("使用配置: 账号=%s 房间=%s 座位=%s", cfg.Username, cfg.RoomID, cfg.SeatNum)
	}

	switch cmd {
	case "check":
		if err := cfg.Validate("check"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		cmdCheck(cfg)

	case "book":
		if err := cfg.Validate("book"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		s, e := cfg.StartTime, cfg.EndTime
		if *start != "" {
			s = *start
		}
		if *end != "" {
			e = *end
		}
		var seatList []string
		if *seats != "" {
			for _, x := range strings.Split(*seats, ",") {
				seatList = append(seatList, strings.TrimSpace(x))
			}
		}
		cmdBook(cfg, s, e, seatList, *captchaCode)

	case "info":
		if err := cfg.Validate("check"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		cmdInfo(cfg)

	case "renew":
		if err := cfg.Validate("renew"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		cmdRenew(cfg)

	case "submitx":
		if err := cfg.Validate("book"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		s, e := cfg.StartTime, cfg.EndTime
		if *start != "" {
			s = *start
		}
		if *end != "" {
			e = *end
		}
		variant := 0
		if *captchaCode != "" {
			if v, perr := strconv.Atoi(*captchaCode); perr == nil {
				variant = v
			}
		}
		cmdSubmitX(cfg, s, e, variant)

	case "fetchpath":
		if err := cfg.Validate("check"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		args := fs.Args()
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "用法: booking.exe fetchpath POST|/path|k=v&k2=v2")
			os.Exit(2)
		}
		cmdFetchPath(cfg, args[0])

	case "signback":
		if err := cfg.Validate("check"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		var rid int64
		if *captchaCode != "" {
			v, perr := strconv.ParseInt(*captchaCode, 10, 64)
			if perr != nil {
				errf("-captcha 应为预约 ID (数字)")
				os.Exit(1)
			}
			rid = v
		}
		cmdSignBack(cfg, rid)

	case "sign":
		if err := cfg.Validate("check"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		var rid int64
		if *captchaCode != "" {
			v, perr := strconv.ParseInt(*captchaCode, 10, 64)
			if perr != nil {
				errf("-captcha 应为预约 ID (数字): %v", perr)
				os.Exit(1)
			}
			rid = v
		}
		cmdSign(cfg, rid, *qrPath)

	case "myreserves":
		if err := cfg.Validate("check"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		cmdMyReserves(cfg)

	case "listpage":
		if err := cfg.Validate("check"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		cmdListPage(cfg)

	case "codepage":
		if err := cfg.Validate("check"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		cmdCodePage(cfg, *captchaCode)

	case "qr":
		args := fs.Args()
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "用法: booking.exe qr <图片路径>")
			os.Exit(2)
		}
		cmdQR(args[0])

	case "cancel":
		if err := cfg.Validate("check"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		cmdCancel(cfg, *captchaCode)

	case "index":
		if err := cfg.Validate("check"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		cmdIndex(cfg)

	case "captcha":
		if err := cfg.Validate("check"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		cmdCaptcha(cfg)

	case "probe":
		if err := cfg.Validate("book"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		s, e := cfg.StartTime, cfg.EndTime
		if *start != "" {
			s = *start
		}
		if *end != "" {
			e = *end
		}
		cmdProbe(cfg, s, e, *captchaCode, *waitCaptcha)

	case "watch":
		if err := cfg.Validate("watch"); err != nil {
			errf("%v", err)
			os.Exit(1)
		}
		cmdWatch(cfg)

	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", cmd)
		usage()
		os.Exit(2)
	}
}
