<template>
  <div class="login-wrap">
    <div class="login-card">
      <img class="logo-icon" :src="logo" alt="iSeat" />
      <h2>自习室自动占座</h2>
      <p class="muted">使用超星(学习通)账号登录</p>
      <form @submit.prevent="doLogin">
        <label class="label">账号</label>
        <input class="input" v-model="username" placeholder="手机号 / 学号" autocomplete="username" />
        <label class="label">密码</label>
        <input class="input" type="password" v-model="password" placeholder="密码" autocomplete="current-password" />
        <button class="btn btn-primary" style="width:100%;margin-top:22px;padding:12px" :disabled="loading">
          {{ loading ? '登录中...' : '登 录' }}
        </button>
        <div v-if="error" class="msg err">{{ error }}</div>
      </form>
      <p class="muted" style="margin-top:16px;text-align:center">登录即代表同意个人账号自动化操作，请遵守学校规则</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { api, setToken } from './api'
import logo from './assets/logo.png'

const emit = defineEmits(['done'])
const username = ref('')
const password = ref('')
const loading = ref(false)
const error = ref('')

async function doLogin() {
  if (!username.value || !password.value) { error.value = '请输入账号和密码'; return }
  loading.value = true
  error.value = ''
  try {
    const res = await api<{ token: string }>('/login', {
      method: 'POST',
      body: JSON.stringify({ username: username.value, password: password.value })
    })
    setToken(res.token)
    emit('done')
  } catch (e: any) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-wrap {
  min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 20px;
}
.login-card {
  background: #fff; border-radius: 28px; padding: 40px 36px; width: 380px; max-width: 92vw;
  box-shadow: 0 16px 50px rgba(59, 92, 155, 0.14); text-align: center;
}
.logo-icon {
  width: 72px; height: 72px; border-radius: 22px; object-fit: cover;
  margin: 0 auto 16px; box-shadow: 0 8px 20px rgba(59, 124, 255, 0.35);
}
h2 { font-size: 20px; margin-bottom: 6px; }
form { text-align: left; margin-top: 18px; }
</style>
