<template>
  <div class="page" :class="{ 'is-login': isLogin }">
    <NavBar v-if="!isLogin" />
    <router-view />
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import NavBar from './components/NavBar.vue'

const route = useRoute()
const isLogin = computed(() => route.path === '/login')
</script>