import { createRouter, createWebHistory } from 'vue-router'
import { isAuthed } from '../store/auth'

import Login from '../views/Login.vue'
import Docs from '../views/Docs.vue'
import Chat from '../views/Chat.vue'
import Impact from '../views/Impact.vue'
import Tasks from '../views/Tasks.vue'
import Repos from '../views/Repos.vue'
import Security from '../views/Security.vue'
import DocEdit from '../views/DocEdit.vue'
import DocHistory from '../views/DocHistory.vue'
import DocSource from '../views/DocSource.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: Login, meta: { public: true } },
    { path: '/', redirect: '/docs' },
    { path: '/docs', name: 'docs', component: Docs },
    { path: '/chat', name: 'chat', component: Chat },
    { path: '/impact', name: 'impact', component: Impact },
    { path: '/tasks', name: 'tasks', component: Tasks },
    { path: '/repos', name: 'repos', component: Repos },
    { path: '/security', name: 'security', component: Security },
    { path: '/doc-edit/:id', name: 'doc-edit', component: DocEdit },
    { path: '/doc-history/:id', name: 'doc-history', component: DocHistory },
    { path: '/doc-source/:id', name: 'doc-source', component: DocSource },
    { path: '/:pathMatch(.*)*', redirect: '/docs' }
  ]
})

router.beforeEach((to) => {
  if (!to.meta.public && !isAuthed()) {
    return { path: '/login', query: to.fullPath === '/' ? {} : { redirect: to.fullPath } }
  }
  if (to.path === '/login' && isAuthed()) {
    return { path: '/docs' }
  }
  return true
})

export default router