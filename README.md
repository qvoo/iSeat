# iSeat · 超星图书馆座位自动预约系统

> 超星（学习通）自习室座位自动预约系统：自动抢座、一键签到、续约到闭馆、共享二维码库。
> 支持 **CLI 命令行** 与 **Web 管理系统** 双形态，Docker 一键部署。

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

---

## ✨ 功能特性

| 功能 | 说明 |
| --- | --- |
| 🎯 自动抢座 | 到点自动高频重试，签名参数 + 行为验证自动完成 |
| 📝 自动签到 | 预约生效窗口自动签到，无需手动扫码 |
| 🔁 自动续约 | 每段 4 小时自动衔接，自动适配每个自习室真实的开放/闭馆时间 |
| 📅 跨天循环 | 今日占座到闭馆后自动预约明日，天天循环（任务引擎无人值守） |
| 🏫 自习室列表 | 自动拉取全部自习室及其开放/闭馆时间 |
| 🪑 座位网格 | 亮=可选、暗=占用的可视化方块，点选代替手输 |
| 📚 共享二维码库 | 上传的桌子二维码自动入库，全员免拍照直接调用 |
| ⚡ 快速预约 | 预约过的桌子一键续约（今日/明日/每天） |
| 🗂 任务管理 | 暂停/恢复/删除任务，手动取消/退座 |
| 💻 CLI / 🌐 Web | 命令行工具 + Vue3 管理系统 |

## 🧱 技术栈

- **后端**：Go + Gin（REST API + 静态托管 + 任务调度器）
- **前端**：Vue 3 + TypeScript + Vite
- **数据库**：MySQL（生产） / SQLite（本地兜底）
- **部署**：Docker / docker-compose

```
server/            Go 后端
  ├─ cxclient.go   超星接口封装（登录/抢座/签到/退座/房间/闭馆遍历）
  ├─ captcha.go    行为验证自动求解
  ├─ qrdecode.go   桌面二维码识别
  ├─ scheduler.go  任务引擎：抢座→签到→续约→跨天循环
  ├─ secret.go     密钥管理与密码加密
  └─ handlers.go   REST API
web/               Vue3+TS 前端
  ├─ Login.vue     超星账号登录
  └─ Dashboard.vue 四大功能 + 共享二维码库 + 任务管理
schema.sql         MySQL 建库脚本
Dockerfile         多阶段构建镜像
docker-compose.yml MySQL + Web 一键部署
```

## 🚀 快速开始

### 方式一：Docker Compose（推荐）
```bash
git clone https://github.com/qvoo/iSeat.git && cd iSeat
# 编辑 docker-compose.yml 修改数据库密码
docker compose up -d --build
# 打开 http://localhost:5251
```

### 方式二：本地运行
```bash
cd server && go build -o seatbook.exe . && ./seatbook.exe   # http://localhost:5251
cd web && npm install && npm run build                       # 前端产物由 Go 自动托管
```

### 方式三：CLI 命令行
```bash
go build -o booking.exe .
./booking.exe check  -c config.json   # 连通性检查
./booking.exe book   -c config.json   # 立即抢座
./booking.exe sign   -c config.json   # 自动签到
./booking.exe renew  -c config.json   # 续约下一时段
./booking.exe watch  -c config.json   # 持续守护：抢座→签到→续约→闭馆
```

## 🖥 Web 系统四大功能

1. **手动选择其他座位**：自习室下拉（自动含闭馆时间）→ 今天/明天切换 → 座位方块网格（亮=可选）→ 确认预约
2. **上传桌子二维码**：拍照上传自动识别 → 占座到闭馆并每日循环；上传即入共享二维码库，全员可直接调用
3. **快速预约**：展示预约过的桌子 → 一键续约今日/明日/每天
4. **任务管理**：暂停/恢复/删除任务；当前预约取消/退座

## 📦 获取 Docker 镜像

### 方式 A：容器仓库直接拉取（推荐）
```bash
docker pull ghcr.io/qvoo/iseat:latest
# 或指定版本
docker pull ghcr.io/qvoo/iseat:v1.0.0

# 运行
docker run -d --name iseat -p 5251:5251 -v iseat_data:/data ghcr.io/qvoo/iseat:latest
```

### 方式 B：下载离线镜像包
从本仓库 **Releases** 下载 `iseat.tar`，然后：
```bash
docker load -i iseat.tar
docker run -d --name iseat -p 5251:5251 -v iseat_data:/data iseat:latest
```

> 提示：两种方式等价；离线 tar 适合内网/无外网环境。

## ⚙️ 配置（环境变量）

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `PORT` | 5251 | 服务端口 |
| `MYSQL_DSN` | 空 | MySQL 连接串；空则 SQLite |
| `SQLITE_PATH` | seatbook.db | SQLite 文件路径 |
| `SEAT_SECRET` | 自动生成 | 本地存储加密密钥（建议生产显式配置） |
| `CX_BASE` / `CX_LOGIN_URL` / `CX_SEAT_ID` | office.chaoxing.com / passport2 fanyalogin / 105 | 超星对接配置 |
| `WEB_DIR` | ../web/dist | 前端静态目录 |
| `TZ` | Asia/Shanghai | 时区 |

## 🛡 安全设计

- **密码加密存储**：账号密码使用 AES-256-GCM 加密后入库
- **密钥独立**：加密密钥从环境变量或独立密钥文件读取，不随仓库分发
- **会话自愈**：超星登录失效自动重登，长期无人值守
- **数据持久化**：数据库/密钥随数据卷持久化，容器重启不丢失
- 生产部署建议启用 HTTPS（反向代理 / Let's Encrypt）保护传输安全

## 🗄 数据库

表：`users`（登录用户）、`session_tokens`（登录会话）、`tasks`（占座任务）、`qr_codes`（共享二维码库）。`schema.sql` 建库，GORM 自动迁移。

## 📮 联系我们

- GitHub：[https://github.com/qvoo/iSeat](https://github.com/qvoo/iSeat)
- 问题反馈：欢迎在仓库提交 **Issue**

## ⚠️ 免责声明

本项目为开源学习工具，仅供学习交流。请使用者遵守学校座位预约规则与相关法律法规，因使用本工具产生的一切后果由使用者自行承担。

## 📄 License

MIT
