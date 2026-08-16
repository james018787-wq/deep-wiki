import { apiRequest } from '../api'

const REPO_KEY = 'ai-code-wiki-repo-id'

export async function fetchRepos() {
  const data = await apiRequest('/repo/list')
  return data || []
}

export function enabledRepos(repos) {
  return (repos || []).filter(r => r.status === 1)
}

export function currentRepoId() {
  return parseInt(localStorage.getItem(REPO_KEY) || '0', 10) || 0
}

export function setRepoId(id) {
  localStorage.setItem(REPO_KEY, String(id))
}

export function defaultRepoId(repos) {
  const cur = currentRepoId()
  const en = enabledRepos(repos)
  if (en.some(r => r.id === cur)) return cur
  return (en[0] && en[0].id) || 0
}

export function repoName(repos, id) {
  const r = (repos || []).find(r => r.id === id)
  return r ? r.repo_name : ('#' + id)
}