-- 自习室自动占座系统 MySQL 建表脚本
-- 服务器部署时使用: mysql -u root -p < schema.sql
-- 后端通过环境变量 MYSQL_DSN 连接（注意：系统会自动建表，此脚本用于初始化数据库）

CREATE DATABASE IF NOT EXISTS seatbook DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE seatbook;

CREATE TABLE IF NOT EXISTS users (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  username VARCHAR(64) NOT NULL UNIQUE COMMENT 'chaoxing账号',
  password VARCHAR(512) NOT NULL COMMENT 'AES加密后的密码',
  uid VARCHAR(32) DEFAULT '',
  created_at DATETIME(3),
  updated_at DATETIME(3)
);

CREATE TABLE IF NOT EXISTS session_tokens (
  token VARCHAR(64) PRIMARY KEY COMMENT '登录令牌',
  user_id BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(3),
  expires_at DATETIME(3),
  INDEX idx_user (user_id)
);

CREATE TABLE IF NOT EXISTS tasks (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  type VARCHAR(16) DEFAULT 'seat' COMMENT 'seat手动/qr扫码/quick快速',
  mode VARCHAR(24) DEFAULT 'today_once' COMMENT 'today_once/tomorrow_once/both/qr',
  room_id VARCHAR(32) NOT NULL,
  seat_id VARCHAR(32) DEFAULT '105',
  seat_num VARCHAR(16) NOT NULL,
  room_name VARCHAR(128) DEFAULT '',
  start_time VARCHAR(8) DEFAULT '08:00',
  duration_minutes INT DEFAULT 240,
  cap_end VARCHAR(8) DEFAULT '22:00' COMMENT '房间闭馆时间(遍历获得)',
  recur_daily TINYINT(1) DEFAULT 0,
  status VARCHAR(16) DEFAULT 'active' COMMENT 'active/paused/done/error',
  last_action TEXT,
  last_ok TINYINT(1) DEFAULT 0,
  reserve_id BIGINT DEFAULT 0,
  reserve_end_at BIGINT DEFAULT 0,
  created_at DATETIME(3),
  updated_at DATETIME(3),
  INDEX idx_user_status (user_id, status)
);

-- 生产环境建议: 定期清理过期 session_token
-- DELETE FROM session_tokens WHERE expires_at < NOW();
