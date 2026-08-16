// 多仓库通用能力：加载仓库列表、读写当前仓库选择（localStorage 跨页共享）。
// 依赖 api.js（apiRequest）。

const REPO_KEY = 'ai-code-wiki-repo-id';

// fetchRepos 获取所有启用仓库。
async function fetchRepos() {
  return (await apiRequest('/repo/list')) || [];
}

// currentRepoId 读取当前选择的仓库 id（未选择返回 0）。
function currentRepoId() {
  const v = parseInt(localStorage.getItem(REPO_KEY) || '', 10);
  return Number.isFinite(v) && v > 0 ? v : 0;
}

// setRepoId 保存当前选择的仓库 id。
function setRepoId(id) {
  if (id > 0) localStorage.setItem(REPO_KEY, String(id));
  else localStorage.removeItem(REPO_KEY);
}

// defaultRepoId 计算默认仓库：优先历史选择，否则取第一个启用仓库。
function defaultRepoId(repos) {
  const cur = currentRepoId();
  const enabled = enabledRepos(repos);
  if (enabled.some(r => r.id === cur)) return cur;
  return (enabled[0] && enabled[0].id) || 0;
}

// enabledRepos 仅保留启用仓库（业务选择器使用）。
function enabledRepos(repos) {
  return (Array.isArray(repos) ? repos : []).filter(r => r.status === 1);
}

// repoName 按 id 取仓库名（用于表格列展示）。
function repoName(repos, id) {
  const r = (Array.isArray(repos) ? repos : []).find(x => x.id === id);
  return r ? r.repo_name : (id ? String(id) : '-');
}