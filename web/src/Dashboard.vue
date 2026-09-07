<template>
  <div class="page">
    <div class="toolbar">
      <div class="logo"><img :src="logo" class="logo-img" alt="iSeat" /> 自习室自动占座系统</div>
      <div class="row" style="gap:10px">
        <button class="btn btn-ghost btn-sm" @click="refreshAll">刷新</button>
        <button class="btn btn-danger btn-sm" @click="logout">退出登录</button>
      </div>
    </div>

    <div class="grid">
      <!-- 功能1: 手动选择其他座位 -->
      <div class="card">
        <h3><span class="icon" style="background:#3b7cff">1</span> 手动选择其他座位</h3>
        <label class="label">自习室</label>
        <select class="select" v-model="manual.roomId" @change="onRoomChange">
          <option value="" disabled>选择自习室…</option>
          <option v-for="r in rooms" :key="r.id" :value="r.id">{{ r.name }}（关闭 {{ r.cap_end || '--' }}）</option>
        </select>
        <div class="row" style="margin-top:12px;gap:8px">
          <span class="pill" :class="{active: manual.day==='today'}" @click="manual.day='today';loadSeats()">今天 {{ todayLabel }}</span>
          <span class="pill" :class="{active: manual.day==='tomorrow'}" @click="manual.day='tomorrow';loadSeats()">明天 {{ tomorrowLabel }}</span>
          <span class="muted grow" style="text-align:right">已选座位：<b style="color:#3b7cff">{{ manual.seatNum || '--' }}</b></span>
        </div>
        <!-- 座位方块网格 -->
        <div style="max-height:230px;overflow:auto;margin-top:10px;border:1px solid #eef1f7;border-radius:14px;padding:10px;background:#fafbfe">
          <div v-if="seatLoading" class="muted" style="text-align:center;padding:20px">座位加载中…</div>
          <div v-else class="seat-grid">
            <div v-for="s in manualSeats" :key="s.num"
                 class="seat" :class="{sel: s.num===manual.seatNum, off: !s.available}"
                 :title="s.available ? '可选' : (s.disabled ? '暂停预约' : '已被占用')"
                 @click="pickSeat(s)">{{ s.num }}</div>
          </div>
          <div class="muted" style="display:flex;gap:14px;justify-content:center;padding-top:8px">
            <span><i class="dot" style="background:#e7f1ff"></i> 可选</span>
            <span><i class="dot" style="background:#d5dae4"></i> 占用/暂停</span>
          </div>
        </div>
        <label class="label">预约模式</label>
        <div class="pills">
          <span class="pill" :class="{active: manual.mode==='today_once'}" @click="manual.mode='today_once'">预约今天</span>
          <span class="pill" :class="{active: manual.mode==='tomorrow_once'}" @click="manual.mode='tomorrow_once'">预约明天</span>
          <span class="pill" :class="{active: manual.mode==='both'}" @click="manual.mode='both'">两个都选·每天自动</span>
        </div>
        <button class="btn btn-primary" style="width:100%;margin-top:18px" :disabled="!manual.seatNum" @click="openConfirm('seat')">确认预约</button>
        <div class="msg" :class="msgManualOk ? 'ok' : 'err'">{{ msg.manual }}</div>
      </div>

      <!-- 功能2: 扫码占座 -->
      <div class="card">
        <h3><span class="icon" style="background:#22a06b">2</span> 上传桌子二维码</h3>
        <label class="upload" @click="fileRef?.click()">
          <input type="file" accept="image/*" hidden ref="fileRef" @change="onQrFile" />
          <img v-if="qrPreview" :src="qrPreview" />
          <template v-else>点击上传桌面二维码照片<br/>（识别后自动占座到闭馆，每日循环）</template>
        </label>
        <div v-if="qrInfo" class="msg ok">已识别：房间 {{ qrInfo.room_id }} · 座位 {{ qrInfo.seat_num }}</div>
        <button class="btn btn-primary" style="width:100%;margin-top:18px" :disabled="!qrInfo" @click="openConfirm('qr')">确认预约</button>
        <div class="msg" :class="msgManualOk ? 'ok' : 'err'">{{ msg.qr }}</div>

        <!-- 共享二维码库 -->
        <div style="margin-top:20px;border-top:1px solid #f0f3f9;padding-top:14px">
          <h3 style="font-size:14px;margin-bottom:4px">📚 共享二维码库</h3>
          <p class="muted" style="margin-bottom:8px">大家上传过的座位二维码，可直接调用（无需再拍照上传）</p>
          <div v-if="qrCodes.length === 0" class="muted">库为空，上传第一张二维码后自动入库</div>
          <div v-for="q in qrCodes" :key="q.id" class="list-item">
            <span class="tag blue">座位{{ q.seat_num }}</span>
            <span class="grow">{{ q.room_name || '房间' + q.room_id }} <span class="muted">· 闭馆 {{ q.cap_end }} · 已用{{ q.upload_count }}次</span></span>
            <button class="btn btn-primary btn-sm" @click="openQrLib(q)">使用</button>
          </div>
        </div>
      </div>

      <!-- 功能3: 快速预约 -->
      <div class="card">
        <h3><span class="icon" style="background:#e08f1f">3</span> 快速预约</h3>
        <p class="muted" style="margin-bottom:8px">选择预约过的桌子，一键续约今日/明日</p>
        <div v-if="nearReserves.length === 0" class="muted">暂无预约记录</div>
        <div v-for="r in nearReserves.slice(0, 12)" :key="r.id" class="list-item">
          <span class="tag blue">座位{{ r.seatNum }}</span>
          <span class="grow">{{ r.secondLevelName }}-{{ r.thirdLevelName }} {{ new Date(r.startTime).toLocaleDateString('zh-CN') }} <span class="tag green">{{ STATUS_TEXT[r.status] || r.status }}</span></span>
          <button class="btn btn-ghost btn-sm" @click="openQuick(r)">快速预约</button>
        </div>
      </div>

      <!-- 功能4: 任务管理 -->
      <div class="card">
        <h3><span class="icon" style="background:#8a6cf0">4</span> 任务管理</h3>
        <div v-if="tasks.length === 0" class="muted">暂无占座任务</div>
        <div v-for="t in tasks" :key="t.id" class="list-item">
          <span class="tag blue">座位{{ t.seat_num }}</span>
          <span class="grow">
            {{ t.room_name || t.room_id }} · <span class="muted">{{ modeText(t.mode) }}</span><br/>
            <span class="muted">{{ t.last_action }}</span>
          </span>
          <span class="tag" :class="t.status==='active' ? 'green' : 'gray'">{{ t.status === 'active' ? '运行中' : t.status === 'paused' ? '已暂停' : '已结束' }}</span>
          <button v-if="t.status==='active'" class="btn btn-ghost btn-sm" @click="taskAction(t,'pause')">暂停</button>
          <button v-else class="btn btn-ghost btn-sm" @click="taskAction(t,'resume')">恢复</button>
          <button class="btn btn-danger btn-sm" @click="taskAction(t,'remove')">删除</button>
        </div>
        <div style="margin-top:14px;border-top:1px solid #f0f3f9;padding-top:12px">
          <h3 style="font-size:14px;margin-bottom:6px">当前预约</h3>
          <div v-if="curReserves.length === 0" class="muted">无进行中的预约</div>
          <div v-for="r in curReserves" :key="r.id" class="list-item">
            <span class="tag blue">座位{{ r.seatNum }}</span>
            <span class="grow">{{ new Date(r.startTime).toLocaleString('zh-CN') }} ~ {{ new Date(r.endTime).toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit'}) }} <span class="tag" :class="r.status===1?'green':'orange'">{{ STATUS_TEXT[r.status]||r.status }}</span></span>
            <button class="btn btn-danger btn-sm" @click="reserveAction(r.id,'cancel')" :disabled="r.status===1||r.status===3||r.status===5">取消</button>
            <button class="btn btn-ghost btn-sm" @click="reserveAction(r.id,'signback')" :disabled="!(r.status===1||r.status===3||r.status===5)">退座</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 免责声明 -->
    <div class="footer">
      <p>免责声明：本系统仅用于本人账号的座位预约自动化操作，请在遵守所在学校座位预约规则的前提下使用；因使用本工具产生的违约、风控或其他后果由使用者自行承担。本项目为开源学习工具，代码仅供学习交流。</p>
      <p style="margin-top:4px">开源地址：<a href="https://github.com/qvoo/iSeat" target="_blank" rel="noopener">https://github.com/qvoo/iSeat</a> · 欢迎提交 Issue 反馈问题</p>
    </div>

    <!-- 确认弹窗 -->
    <div v-if="confirm.open" class="mask" @click.self="confirm.open=false">
      <div class="dialog">
        <h4>确认预约</h4>
        <div v-if="confirm.info" style="font-size:13.5px" class="muted">
          自习室：{{ confirm.info.roomName }}<br/>
          座位号：<b style="color:#3b7cff">{{ confirm.info.seatNum }}</b> · 闭馆：{{ confirm.info.capEnd }}
        </div>
        <label v-if="confirm.type !== 'qr'" class="label">预约模式</label>
        <div v-if="confirm.type !== 'qr'" class="pills">
          <span class="pill" :class="{active: confirm.mode==='today_once'}" @click="confirm.mode='today_once'">预约今天</span>
          <span class="pill" :class="{active: confirm.mode==='tomorrow_once'}" @click="confirm.mode='tomorrow_once'">预约明天</span>
          <span class="pill" :class="{active: confirm.mode==='both'}" @click="confirm.mode='both'">两个都选·每天自动</span>
        </div>
        <div class="btns">
          <button class="btn btn-ghost" @click="confirm.open=false">再想想</button>
          <button class="btn btn-primary" :disabled="confirm.submitting" @click="submitConfirm">确认预约</button>
        </div>
        <div v-if="confirm.error" class="msg err">{{ confirm.error }}</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { api, clearToken, STATUS_TEXT, type Room, type Task, type Reserve, type QrCode } from './api'
import logo from './assets/logo.png'

const emit = defineEmits(['logout'])
const rooms = ref<Room[]>([])
const nearReserves = ref<Reserve[]>([])
const curReserves = ref<Reserve[]>([])
const tasks = ref<Task[]>([])
const qrCodes = ref<QrCode[]>([])

const manual = reactive({ roomId: '', seatNum: '', mode: 'today_once', day: 'today' })
const manualSeats = ref<{ num: string; available: boolean }[]>([])
const seatLoading = ref(false)
const msg = reactive({ manual: '', qr: '' })
const msgManualOk = ref(true)

// 今天/明天 日期标签
const todayLabel = ref(formatDateCN(new Date()))
const tomorrowLabel = ref(formatDateCN(new Date(Date.now() + 86400000)))
const currentDayLabel = ref('') // 当前查看的完整日期

function formatDateCN(d: Date): string {
  return `${d.getMonth() + 1}月${d.getDate()}日`
}

function dayStr(offset: number): string {
  const d = new Date(Date.now() + offset * 86400000)
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${d.getFullYear()}-${m}-${day}`
}

const fileRef = ref<HTMLInputElement>()
const qrPreview = ref('')
const qrImage = ref('')
const qrInfo = ref<{ room_id: string; seat_num: string } | null>(null)

const confirm = reactive({
  open: false,
  type: '',
  mode: 'today_once',
  submitting: false,
  error: '',
  info: null as null | { roomId: string; seatId: string; seatNum: string; roomName: string; capEnd: string }
})

function modeText(m: string) {
  return { today_once: '预约今天', tomorrow_once: '预约明天', both: '每天自动占座', qr: '扫码·占座到闭馆' }[m] || m
}

async function refreshAll() {
  try {
    const [r, m, t, q] = await Promise.all([
      api<{ rooms: Room[] }>('/rooms'),
      api<{ cur: Reserve[]; near: Reserve[] }>('/my-reserves'),
      api<{ tasks: Task[] }>('/tasks'),
      api<{ qr_codes: QrCode[] }>('/qr-codes')
    ])
    rooms.value = r.rooms
    curReserves.value = m.cur
    nearReserves.value = m.near
    tasks.value = t.tasks
    qrCodes.value = q.qr_codes
    if (manual.roomId) loadSeats()
  } catch (e: any) {
    if (String(e.message).includes('未登录')) logout()
  }
}

function onRoomChange() {
  manual.seatNum = ''
  loadSeats()
}

// 加载座位网格（亮=可选）
async function loadSeats() {
  const room = rooms.value.find(r => r.id === manual.roomId)
  if (!room) { manualSeats.value = []; return }
  seatLoading.value = true
  try {
    const day = manual.day === 'tomorrow' ? dayStr(1) : dayStr(0)
    currentDayLabel.value = day
    const q = new URLSearchParams({ day })
    const res = await api<{ seats: { num: string; available: boolean }[] }>(`/rooms/${manual.roomId}/seats?${q}`)
    manualSeats.value = res.seats
    msgManualOk.value = true
    msg.manual = `${day} 共 ${res.seats.length} 个座位 · 可选 ${res.seats.filter(s => s.available).length} 个`
  } catch (e: any) {
    msgManualOk.value = false
    msg.manual = '座位加载失败: ' + e.message
  } finally {
    seatLoading.value = false
  }
}

function pickSeat(s: { num: string; available: boolean }) {
  if (!s.available) return
  manual.seatNum = manual.seatNum === s.num ? '' : s.num
}

function openConfirm(type: string) {
  if (type === 'seat') {
    const room = rooms.value.find(r => r.id === manual.roomId)
    if (!room) { msgManualOk.value = false; msg.manual = '请先选择自习室'; return }
    if (!manual.seatNum) { msgManualOk.value = false; msg.manual = '请点击方块选择座位'; return }
    confirm.info = { roomId: room.id, seatId: '105', seatNum: manual.seatNum, roomName: room.name, capEnd: room.cap_end || '--' }
    confirm.mode = manual.mode
  } else if (type === 'qr') {
    if (!qrInfo.value) return
    confirm.info = { roomId: qrInfo.value.room_id, seatId: '105', seatNum: qrInfo.value.seat_num, roomName: '二维码识别', capEnd: '' }
    confirm.mode = 'qr'
  }
  confirm.type = type
  confirm.error = ''
  confirm.open = true
}

function openQuick(r: Reserve) {
  confirm.mode = 'today_once'
  confirm.type = 'quick'
  confirm.info = { roomId: r.roomId, seatId: '105', seatNum: r.seatNum, roomName: r.secondLevelName + '-' + r.thirdLevelName, capEnd: '' }
  confirm.error = ''
  confirm.open = true
}

// 使用共享二维码库
function openQrLib(q: QrCode) {
  confirm.mode = 'both'
  confirm.type = 'qr'
  confirm.info = { roomId: q.room_id, seatId: q.seat_id || '105', seatNum: q.seat_num, roomName: '二维码库·' + (q.room_name || '房间' + q.room_id), capEnd: q.cap_end }
  confirm.error = ''
  confirm.open = true
}

async function submitConfirm() {
  confirm.submitting = true
  confirm.error = ''
  try {
    const body: any = {
      type: confirm.type,
      mode: confirm.mode,
      room_id: confirm.info!.roomId,
      seat_id: '105',
      seat_num: confirm.info!.seatNum,
      room_name: confirm.info!.roomName,
      start_time: '08:00',
      duration_minutes: 240,
      recur_daily: confirm.mode === 'both' || confirm.mode === 'qr'
    }
    if (confirm.type === 'qr' && qrImage.value) body.qr_image = qrImage.value
    const res = await api<{ task: Task }>('/tasks', { method: 'POST', body: JSON.stringify(body) })
    confirm.open = false
    msgManualOk.value = true
    msg.manual = `任务 #${res.task.id} 已创建并开始运作`
    msg.qr = `任务 #${res.task.id} 已创建并开始运作`
    await refreshAll()
  } catch (e: any) {
    confirm.error = e.message
  } finally {
    confirm.submitting = false
  }
}

function onQrFile(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (!f) return
  const reader = new FileReader()
  reader.onload = () => {
    const data = reader.result as string
    qrPreview.value = data
    qrImage.value = data.split(',')[1] || data
    qrInfo.value = { room_id: '待服务端识别', seat_num: '…' }
    msg.qr = '已上传，点击"确认预约"后自动识别二维码并开始占座'
  }
  reader.readAsDataURL(f)
}

async function taskAction(t: Task, action: string) {
  await api(`/tasks/${t.id}/action`, { method: 'POST', body: JSON.stringify({ action }) })
  await refreshAll()
}

async function reserveAction(reserveId: number, action: string) {
  try {
    await api('/reserve-action', { method: 'POST', body: JSON.stringify({ action, reserve_id: reserveId }) })
  } catch (e) {
    alert((e as any).message)
  }
  await refreshAll()
}

function logout() {
  clearToken()
  emit('logout')
}

onMounted(refreshAll)
</script>

<style scoped>
.page { padding: 22px; max-width: 1080px; margin: 0 auto; }
.logo-img { width: 38px; height: 38px; border-radius: 12px; object-fit: cover; }
</style>
