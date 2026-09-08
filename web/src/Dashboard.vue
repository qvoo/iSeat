<template>
  <div class="page">
    <div class="toolbar">
      <div class="logo"><img :src="logo" class="logo-img" alt="iSeat" /> 自习室自动占座系统</div>
      <div class="row" style="gap:10px;flex:none;width:auto">
        <span v-if="newTaskMsg" class="muted" style="color:#22a06b">{{ newTaskMsg }}</span>
        <button class="btn btn-ghost btn-sm" @click="refreshAll">刷新</button>
        <button class="btn btn-danger btn-sm" @click="logout">退出登录</button>
      </div>
    </div>

    <div class="grid">
      <!-- 功能1: 手动选座（含作用域） -->
      <div class="card">
        <h3><span class="icon" style="background:#3b7cff">1</span> 手动选择其他座位</h3>
        <div class="row" style="gap:8px;align-items:center">
          <span class="muted">作用域</span>
          <span class="pill" :class="{active: scope.mode==='single'}" @click="scope.mode='single';onScopeChange()">单账号</span>
          <span class="pill" :class="{active: scope.mode==='all'}" @click="scope.mode='all';onScopeChange()">全部账号·批量</span>
          <span class="grow"></span>
          <button class="btn btn-ghost btn-sm" @click="showAccPop=!showAccPop">管理账号</button>
        </div>
        <div v-if="scope.mode==='single'" class="label" style="margin-top:8px">
          作用于账号
          <select class="select" style="margin-top:4px" v-model="scope.accountId" @change="onScopeChange">
            <option v-for="a in accounts" :key="a.id" :value="a.id">{{ a.username }}</option>
          </select>
        </div>
        <div v-else class="muted" style="margin-top:8px">共 {{ accounts.length }} 个账号，将批量占座（座位自动分配）</div>

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
        <p class="muted" style="margin-top:8px">签到无需现场扫码，到签到时间系统自动完成。</p>
      </div>

      <!-- 功能2: 任务管理 -->
      <div class="card">
        <h3><span class="icon" style="background:#8a6cf0">2</span> 任务管理</h3>
        <div v-if="scopedTasks.length === 0" class="muted">暂无占座任务{{ scope.mode==='single' ? '（' + currentName + '）' : '' }}</div>
        <div v-for="t in scopedTasks" :key="t.id" class="list-item">
          <span class="tag blue">座位{{ t.seat_num }}</span>
          <span class="grow">
            <b>{{ roomsMap[t.room_id] || t.room_name || '房间 '+t.room_id }}</b> · <span class="muted">{{ modeText(t.mode) }}</span>
            <span v-if="t.username" class="muted"> | {{ t.username }}</span><br/>
            <span class="muted">{{ t.last_action }}</span>
          </span>
          <span class="tag" :class="t.status==='active' ? 'green' : 'gray'">{{ t.status === 'active' ? '运行中' : t.status === 'paused' ? '已暂停' : '已结束' }}</span>
          <button v-if="t.status==='active'" class="btn btn-ghost btn-sm" @click="taskAction(t,'pause')">暂停</button>
          <button v-else class="btn btn-ghost btn-sm" @click="taskAction(t,'resume')">恢复</button>
          <button class="btn btn-danger btn-sm" @click="taskAction(t,'remove')">删除</button>
        </div>

        <div style="margin-top:14px;border-top:1px solid #f0f3f9;padding-top:12px">
          <h3 style="font-size:14px;margin-bottom:6px">当前预约（{{ scope.mode==='all' ? '全部账号' : currentName }}）</h3>
          <div v-if="curReserves.length === 0" class="muted">无进行中的预约</div>
          <div v-for="r in curReserves" :key="r.id" class="list-item">
            <span class="tag blue">座位{{ r.seatNum }}</span>
            <span class="grow">{{ new Date(r.startTime).toLocaleString('zh-CN') }} ~ {{ new Date(r.endTime).toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit'}) }} <span class="muted">· {{ r.secondLevelName }}-{{ r.thirdLevelName }}</span> <span class="tag" :class="r.status===1?'green':'orange'">{{ STATUS_TEXT[r.status]||r.status }}</span><span v-if="r.username" class="muted"> · {{ r.username }}</span></span>
            <button class="btn btn-danger btn-sm" @click="reserveAction(r, 'cancel')" :disabled="r.status===1||r.status===3||r.status===5">取消</button>
            <button class="btn btn-ghost btn-sm" @click="reserveAction(r, 'signback')" :disabled="!(r.status===1||r.status===3||r.status===5)">退座</button>
          </div>
        </div>
      </div>

      <!-- 功能3: 快速预约 -->
      <div class="card">
        <h3><span class="icon" style="background:#e08f1f">3</span> 快速预约</h3>
        <p class="muted" style="margin-bottom:8px">展示{{ scope.mode==='all' ? '全部账号' : currentName }}预约过的桌子，一键续约。</p>
        <div v-if="nearReserves.length === 0" class="muted">暂无预约记录</div>
        <div v-for="r in nearReserves.slice(0, 12)" :key="r.id" class="list-item">
          <span class="tag blue">座位{{ r.seatNum }}</span>
          <span class="grow">{{ r.secondLevelName }}-{{ r.thirdLevelName }} {{ new Date(r.startTime).toLocaleDateString('zh-CN') }} <span class="tag green">{{ STATUS_TEXT[r.status] || r.status }}</span><span v-if="r.username" class="muted"> · {{ r.username }}</span></span>
          <button class="btn btn-ghost btn-sm" @click="openQuick(r)">快速预约</button>
        </div>
      </div>
    </div>

    <div class="footer">
      <p>免责声明：本系统仅用于本人账号的座位预约自动化操作，请在遵守所在学校座位预约规则的前提下使用；因使用本工具产生的违约、风控或其他后果由使用者自行承担。本项目为开源学习工具，代码仅供学习交流。</p>
      <p style="margin-top:4px">开源地址：<a href="https://github.com/qvoo/iSeat" target="_blank" rel="noopener">https://github.com/qvoo/iSeat</a> · 欢迎提交 Issue 反馈问题</p>
    </div>

    <!-- 账号管理浮层 -->
    <div v-if="showAccPop" class="acc-pop">
      <h3 style="font-size:15px;margin-bottom:12px">账号管理</h3>
      <div class="row" style="gap:8px">
        <input class="input" v-model="newAcc.username" placeholder="手机号/学号" style="flex:1" />
        <input class="input" type="password" v-model="newAcc.password" placeholder="密码" style="flex:1" />
      </div>
      <p class="muted" style="margin-top:6px;font-size:12px">学校参数（选填，留空用默认）：每个账号可绑定自己的学校，跨校自动用该校座位/房间。</p>
      <div class="row" style="gap:6px;margin-top:6px;flex-wrap:wrap">
        <input class="input input-sm" v-model="newAcc.seat_id" placeholder="seat_id 默认105" />
        <input class="input input-sm" v-model="newAcc.dept_id_enc" placeholder="dept_id_enc" />
        <input class="input input-sm" v-model="newAcc.seat_id_enc" placeholder="seat_id_enc" />
        <input class="input input-sm" v-model="newAcc.captcha_id" placeholder="captcha_id" />
      </div>
      <button class="btn btn-primary btn-sm" style="width:100%;margin-top:8px" @click="addAccount" :disabled="addingAcc">添加账号</button>
      <div style="margin-top:10px;max-height:300px;overflow:auto">
        <div v-for="a in accounts" :key="a.id" class="list-item" style="padding:10px 0">
          <span class="tag blue">{{ a.username }}</span>
          <span class="tag" :class="a.id===currentUser?'green':'gray'">{{ a.id===currentUser?'我':'批量' }}</span>
          <span class="tag gray grow" style="flex:0 0 auto;max-width:130px;overflow:hidden;text-overflow:ellipsis">{{ a.school || '默认学校' }}</span>
          <button class="btn btn-ghost btn-sm" @click="toggleSchoolEdit(a)">学校</button>
          <button class="btn btn-danger btn-sm" @click="delAccount(a.id)">删除</button>
        </div>
        <template v-for="a in accounts" :key="'e'+a.id">
          <div v-if="schoolEdit[a.id]?.open" class="school-edit" style="margin-bottom:8px">
            <div class="row" style="gap:6px;flex-wrap:wrap">
              <input class="input input-sm" v-model="schoolEdit[a.id].seat_id" placeholder="seat_id" />
              <input class="input input-sm" v-model="schoolEdit[a.id].dept_id_enc" placeholder="dept_id_enc" />
              <input class="input input-sm" v-model="schoolEdit[a.id].seat_id_enc" placeholder="seat_id_enc" />
              <input class="input input-sm" v-model="schoolEdit[a.id].captcha_id" placeholder="captcha_id" />
            </div>
            <div class="row" style="gap:8px;margin-top:6px">
              <button class="btn btn-ghost btn-sm" @click="schoolEdit[a.id].open=false">取消</button>
              <button class="btn btn-primary btn-sm" @click="saveSchool(a)">保存学校</button>
            </div>
          </div>
        </template>
      </div>
      <p class="muted" style="margin-top:6px">作用域为"全部账号"时，三个模块批量给每个账号占座（座位自动分配）。不同学校账号请分到对应房间再批量。</p>
    </div>

    <!-- 确认弹窗 -->
    <div v-if="confirm.open" class="mask" @click.self="confirm.open=false">
      <div class="dialog">
        <h4>确认预约<span v-if="confirm.type==='quick'" class="muted" style="font-weight:400">（{{ accounts.find(a=>a.id===confirm.accountId)?.username || '该账号' }}）</span><span v-else-if="scope.mode==='all'" class="muted" style="font-weight:400">（全部账号·批量）</span><span v-else class="muted" style="font-weight:400">（{{ currentName }}）</span></h4>
        <div v-if="confirm.info" style="font-size:13.5px" class="muted">
          自习室：{{ confirm.info.roomName }}<br/>
          座位号：<b style="color:#3b7cff">{{ confirm.info.seatNum }}</b> · 闭馆：{{ confirm.info.capEnd }}
          <span v-if="scope.mode==='all' && confirm.type!=='quick'" style="display:block;margin-top:4px">批量模式：该座位给第 1 个账号，其余账号自动分配该房间空闲座位</span>
        </div>
        <label class="label">预约模式</label>
        <div class="pills">
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
import { computed, onMounted, reactive, ref } from 'vue'
import { api, clearToken, STATUS_TEXT, type Room, type Task, type Reserve, type Account } from './api'
import logo from './assets/logo.png'

const emit = defineEmits(['logout'])
const currentUser = ref(0)
const accounts = ref<Account[]>([])
const scope = reactive({ mode: 'single' as 'single' | 'all', accountId: 0 })
const showAccPop = ref(false)
const newAcc = reactive({ username: '', password: '', seat_id: '', dept_id_enc: '', seat_id_enc: '', captcha_id: '' })
const addingAcc = ref(false)
const newTaskMsg = ref('')
const mySeatId = ref('105')

const rooms = ref<Room[]>([])
const nearReserves = ref<(Reserve & { username?: string })[]>([])
const curReserves = ref<(Reserve & { username?: string })[]>([])
const tasks = ref<Task[]>([])

const currentName = computed(() => {
  const a = accounts.value.find(x => x.id === scope.accountId)
  return a ? a.username : '当前账号'
})
const roomsMap = computed<Record<string, string>>(() => {
  const m: Record<string, string> = {}
  for (const r of rooms.value) m[r.id] = r.name
  return m
})
const scopedTasks = computed(() => {
  if (scope.mode === 'single') return tasks.value.filter(t => t.user_id === scope.accountId)
  return tasks.value
})

// 账号学校参数（管理浮层编辑用）
const schoolEdit = reactive<Record<number, { open: boolean; seat_id: string; dept_id_enc: string; seat_id_enc: string; captcha_id: string }>>({})
function toggleSchoolEdit(a: Account) {
  if (!schoolEdit[a.id]) schoolEdit[a.id] = { open: false, seat_id: '', dept_id_enc: '', seat_id_enc: '', captcha_id: '' }
  schoolEdit[a.id].open = !schoolEdit[a.id].open
  schoolEdit[a.id].seat_id = a.seat_id || ''
  schoolEdit[a.id].dept_id_enc = a.dept_id_enc || ''
  schoolEdit[a.id].seat_id_enc = a.seat_id_enc || ''
  schoolEdit[a.id].captcha_id = a.captcha_id || ''
}
function schoolSeatId(accountId: number): string {
  const a = accounts.value.find(x => x.id === accountId)
  return (a && a.seat_id) || mySeatId.value
}

const manual = reactive({ roomId: '', seatNum: '', mode: 'today_once', day: 'today' })
const manualSeats = ref<{ num: string; available: boolean }[]>([])
const seatLoading = ref(false)
const msg = reactive({ manual: '' })
const msgManualOk = ref(true)
const todayLabel = ref(formatDateCN(new Date()))
const tomorrowLabel = ref(formatDateCN(new Date(Date.now() + 86400000)))

function formatDateCN(d: Date): string { return `${d.getMonth() + 1}月${d.getDate()}日` }
function dayStr(offset: number): string {
  const d = new Date(Date.now() + offset * 86400000)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const confirm = reactive({
  open: false, type: '', mode: 'today_once', submitting: false, error: '', accountId: 0,
  info: null as null | { roomId: string; seatId: string; seatNum: string; roomName: string; capEnd: string }
})

function modeText(m: string) {
  return { today_once: '预约今天', tomorrow_once: '预约明天', both: '每天自动占座', qr: '扫码·占座到闭馆' }[m] || m
}

async function refreshAll() {
  try {
    const roomsQ = scope.mode === 'single' && scope.accountId ? `?account_id=${scope.accountId}` : ''
    const [r, acc, t] = await Promise.all([
      api<{ rooms: Room[] }>(`/rooms${roomsQ}`),
      api<{ accounts: Account[] }>('/accounts'),
      api<{ tasks: Task[] }>('/tasks')
    ])
    rooms.value = r.rooms
    accounts.value = acc.accounts
    tasks.value = t.tasks
    if (!scope.accountId) { currentUser.value = acc.accounts[0]?.id || 0; scope.accountId = acc.accounts[0]?.id || 0 }
    const cur = accounts.value.find(a => a.id === (scope.mode === 'single' ? scope.accountId : currentUser.value))
    if (cur && cur.seat_id) mySeatId.value = cur.seat_id
    await loadReserves()
    if (manual.roomId) loadSeats()
  } catch (e: any) { if (String(e.message).includes('未登录')) logout() }
}

async function loadReserves() {
  try {
    if (scope.mode === 'single') {
      const m = await api<{ cur: Reserve[]; near: Reserve[] }>(`/my-reserves?account_id=${scope.accountId}`)
      curReserves.value = m.cur.map(x => ({ ...x, username: currentName.value, accountId: scope.accountId }))
      nearReserves.value = m.near.map(x => ({ ...x, username: currentName.value, accountId: scope.accountId }))
    } else {
      const curAcc: (Reserve & { username?: string; accountId?: number })[] = []
      const nearAcc: (Reserve & { username?: string; accountId?: number })[] = []
      for (const a of accounts.value) {
        try {
          const m = await api<{ cur: Reserve[]; near: Reserve[] }>(`/my-reserves?account_id=${a.id}`)
          m.cur.forEach(x => curAcc.push({ ...x, username: a.username, accountId: a.id }))
          m.near.forEach(x => nearAcc.push({ ...x, username: a.username, accountId: a.id }))
        } catch { /* 跳过 */ }
      }
      curReserves.value = curAcc
      nearReserves.value = nearAcc
    }
  } catch { /* ignore */ }
}

function onScopeChange() { refreshAll() }
function onRoomChange() { manual.seatNum = ''; loadSeats() }

async function loadSeats() {
  const room = rooms.value.find(r => r.id === manual.roomId)
  if (!room) { manualSeats.value = []; return }
  seatLoading.value = true
  try {
    const day = manual.day === 'tomorrow' ? dayStr(1) : dayStr(0)
    const q = new URLSearchParams({ day })
    if (scope.mode === 'single' && scope.accountId) q.set('account_id', String(scope.accountId))
    const res = await api<{ seats: { num: string; available: boolean }[] }>(`/rooms/${manual.roomId}/seats?${q}`)
    manualSeats.value = res.seats
    msgManualOk.value = true
    msg.manual = `${day} 共 ${res.seats.length} 个座位 · 可选 ${res.seats.filter(s => s.available).length} 个`
  } catch (e: any) { msgManualOk.value = false; msg.manual = '座位加载失败: ' + e.message }
  finally { seatLoading.value = false }
}

function pickSeat(s: { num: string; available: boolean }) {
  if (!s.available) return
  manual.seatNum = manual.seatNum === s.num ? '' : s.num
}

function openConfirm(type: string) {
  const room = rooms.value.find(r => r.id === manual.roomId)
  if (!room) { msgManualOk.value = false; msg.manual = '请先选择自习室'; return }
  if (!manual.seatNum) { msgManualOk.value = false; msg.manual = '请点击方块选择座位'; return }
  const seatId = schoolSeatId(scope.mode === 'all' ? currentUser.value : scope.accountId)
  confirm.info = { roomId: room.id, seatId, seatNum: manual.seatNum, roomName: room.name, capEnd: room.cap_end || '--' }
  confirm.mode = manual.mode
  confirm.type = type
  confirm.error = ''
  confirm.open = true
}

function openQuick(r: Reserve & { accountId?: number }) {
  confirm.mode = 'today_once'
  confirm.type = 'quick'
  const accountId = r.accountId || scope.accountId
  confirm.accountId = accountId
  // roomId 可能是数字，必须转字符串（后端 room_id 为 string）
  confirm.info = { roomId: String(r.roomId), seatId: schoolSeatId(accountId), seatNum: r.seatNum, roomName: r.secondLevelName + '-' + r.thirdLevelName, capEnd: '' }
  confirm.error = ''
  confirm.open = true
}

async function submitConfirm() {
  confirm.submitting = true
  confirm.error = ''
  const isQuick = confirm.type === 'quick'
  const all = scope.mode === 'all' && !isQuick // 快速预约始终只给归属账号预约
  try {
    const accountId = isQuick ? confirm.accountId : scope.accountId
    const seatId = isQuick || scope.mode === 'single' ? schoolSeatId(accountId) : schoolSeatId(currentUser.value)
    const base = {
      type: confirm.type, mode: confirm.mode,
      room_id: confirm.info!.roomId, seat_id: seatId, seat_num: confirm.info!.seatNum,
      room_name: confirm.info!.roomName, start_time: '08:00', duration_minutes: 240,
      recur_daily: confirm.mode === 'both'
    }
    if (all) {
      const res = await api<{ created: Task[] }>('/batch-task', {
        method: 'POST',
        body: JSON.stringify({ room_id: base.room_id, seat_id: base.seat_id, mode: base.mode, start_time: base.start_time, room_name: base.room_name, seats: [base.seat_num], account_ids: [] })
      })
      newTaskMsg.value = `已为 ${res.created.length} 个账号创建任务`
    } else {
      const res = await api<{ task: Task }>('/tasks', { method: 'POST', body: JSON.stringify({ ...base, account_id: accountId }) })
      newTaskMsg.value = `任务 #${res.task.id} 已创建（${accounts.value.find(a => a.id === accountId)?.username || ''}）`
    }
    confirm.open = false
    await refreshAll()
    setTimeout(() => (newTaskMsg.value = ''), 5000)
  } catch (e: any) { confirm.error = e.message }
  finally { confirm.submitting = false }
}

async function addAccount() {
  if (!newAcc.username || !newAcc.password) { alert('请输入账号密码'); return }
  addingAcc.value = true
  try {
    const body = {
      username: newAcc.username, password: newAcc.password,
      seat_id: newAcc.seat_id || undefined,
      dept_id_enc: newAcc.dept_id_enc || undefined,
      seat_id_enc: newAcc.seat_id_enc || undefined,
      captcha_id: newAcc.captcha_id || undefined
    }
    const res = await api<{ account: Account; msg?: string }>('/accounts', { method: 'POST', body: JSON.stringify(body) })
    if (!res.msg) { newAcc.username = ''; newAcc.password = ''; newAcc.seat_id = ''; newAcc.dept_id_enc = ''; newAcc.seat_id_enc = ''; newAcc.captcha_id = '' }
    await refreshAll()
  } catch (e: any) { alert('添加失败: ' + e.message) } finally { addingAcc.value = false }
}

async function delAccount(id: number) {
  if (!confirm('删除该账号及其所有任务？')) return
  try { await api(`/accounts/${id}`, { method: 'DELETE' }) } catch (e: any) { alert(e.message) }
  if (scope.accountId === id) scope.accountId = accounts.value.find(a => a.id !== id)?.id || 0
  await refreshAll()
}

async function saveSchool(a: Account) {
  const e = schoolEdit[a.id]
  if (!e) return
  try {
    await api(`/accounts/${a.id}/school`, { method: 'PUT', body: JSON.stringify({ seat_id: e.seat_id, dept_id_enc: e.dept_id_enc, seat_id_enc: e.seat_id_enc, captcha_id: e.captcha_id }) })
    e.open = false
    await refreshAll()
  } catch (err: any) { alert('保存失败: ' + err.message) }
}

async function taskAction(t: Task, action: string) {
  await api(`/tasks/${t.id}/action`, { method: 'POST', body: JSON.stringify({ action }) })
  await refreshAll()
}

async function reserveAction(r: Reserve & { accountId?: number }, action: string) {
  try { await api('/reserve-action', { method: 'POST', body: JSON.stringify({ action, reserve_id: r.id, account_id: r.accountId || scope.accountId }) }) }
  catch (e) { alert((e as any).message) }
  await refreshAll()
}

function logout() { clearToken(); emit('logout') }

onMounted(async () => {
  try { const me = await api<{ user: { id: number } }>('/me'); currentUser.value = me.user?.id || 0; scope.accountId = currentUser.value } catch { /* ignore */ }
  await refreshAll()
})
</script>

<style scoped>
.page { padding: 22px; max-width: 1180px; margin: 0 auto; }
.logo-img { width: 38px; height: 38px; border-radius: 12px; object-fit: cover; }
</style>
