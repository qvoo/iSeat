<template>
  <Login v-if="!logged" @done="logged = true" />
  <Dashboard v-else @logout="logged = false" />
</template>

<script setup lang="ts">
import { ref } from 'vue'
import Login from './Login.vue'
import Dashboard from './Dashboard.vue'
import { getToken } from './api'

const logged = ref(!!getToken())
</script>
