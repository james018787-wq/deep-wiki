<template>
  <nav class="nav">
    <router-link class="brand" to="/docs"><img class="brand-logo" src="/logo.svg" alt="logo">AI·CODE WIKI</router-link>
    <div class="links">
      <router-link to="/docs" active-class="active">文档列表</router-link>
      <router-link to="/chat" active-class="active">智能问答</router-link>
      <router-link to="/impact" active-class="active">迭代影响</router-link>
      <router-link to="/tasks" active-class="active">任务管理</router-link>
      <router-link to="/repos" active-class="active">仓库管理</router-link>
      <router-link to="/security" active-class="active">安全扫描</router-link>
      <router-link to="/models" active-class="active">模型与用量</router-link>
    </div>
    <div class="spacer"></div>
    <span class="who">{{ userNick }}</span>
    <a class="logout" href="javascript:void(0)" @click.prevent="doLogout">退出</a>
  </nav>
</template>

<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { getUser, logout } from '../store/auth'

const router = useRouter()
const user = getUser()
const userNick = computed(() => (user && (user.nickname || user.username)) || '用户')

async function doLogout() {
  await logout()
  router.replace('/login')
}
</script>