# ---- 前端构建阶段 ----
FROM node:20-alpine AS webbuild
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm config set registry https://registry.npmjs.org && npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- Go 后端构建阶段 ----
FROM golang:1.23-alpine AS gobuild
WORKDIR /src/server
# GOTOOLCHAIN=auto: 允许按 go.mod 需求自动下载匹配的 Go 工具链；国内走 goproxy.cn 加速
ENV GOPROXY=https://goproxy.cn,direct \
    GOTOOLCHAIN=auto
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -o /out/seatbook .

# ---- 运行阶段 ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai \
    PORT=5251 \
    WEB_DIR=/app/web/dist \
    SQLITE_PATH=/data/seatbook.db
WORKDIR /app
COPY --from=gobuild /out/seatbook /app/seatbook
COPY --from=webbuild /src/web/dist /app/web/dist
VOLUME ["/data"]
EXPOSE 5251
CMD ["/app/seatbook"]
