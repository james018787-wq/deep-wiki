# AI·CODE WIKI API 文档

REST API，统一前缀 `/api/v1`（`/health`、`/auth/login`、`/webhook/git_push` 除外）。

## 鉴权

所有业务接口需携带以下任一凭证（`Authorization` 请求头）：

- **用户令牌**：`Authorization: Bearer <token>`（`/auth/login` 签发）
- **服务令牌**：`Authorization: Bearer <X-Api-Secret>`（server-to-server）

`/webhook/git_push` 使用自有 `WEBHOOK_SECRET` 签名鉴权，不经过统一中间件。

## 统一响应结构

所有接口返回统一 JSON 包裹：

```json
{ "code": 0, "msg": "ok", "data": <业务数据> }
```

- `code`：`0` 成功；非 0 为错误码
- `data` 恒存在（无业务数据时为 `null`）

### 通用分页结构（`data` 内容）

```json
{ "list": [], "total": 0, "page": 1, "page_size": 20 }
```

---

## 鉴权与用户

### POST /auth/login 登录

**请求**

```json
{ "username": "admin", "password": "admin123" }
```

**响应 `data`**

```json
{
  "token": "eyJ...",
  "expire_at": 1757000000,
  "username": "admin",
  "nickname": "管理员",
  "is_admin": true
}
```

### GET /auth/me 当前用户
**响应 `data`**：当前登录用户信息（含 `username`、`nickname`、`is_admin`）。

### POST /auth/logout 登出
**响应 `data`**：`null`。

---

## 仓库管理

### POST /repo/register 注册仓库（幂等）

自动校验可用性（`git ls-remote`：地址可达 + 默认分支存在）。

**请求**

```json
{
  "repo_name": "order-service",
  "repo_url": "https://gitlab.com/group/order.git",
  "default_branch": "main",
  "description": "订单服务",
  "auth_token": "glpat-xxxx"
}
```

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| repo_name | string | ✅ | 仓库名（全局唯一，克隆目录 `{GIT_CLONE_DIR}/{repo_name}`） |
| repo_url | string | ✅ | 克隆地址（https/ssh） |
| default_branch | string | — | 默认分支（默认 `main`，作为增量 diff 基线） |
| description | string | — | 仓库说明 |
| auth_token | string | — | 私有仓库访问令牌（HTTPS Bearer，AES-GCM 加密存储） |

**响应 `data`**：`CodeRepo` 对象（`id`、`repo_name`、`repo_url`、`default_branch`、`description`、`status`、`create_time` 等；`auth_token` 已脱敏）。

### GET /repo/list 仓库列表
**响应 `data`**：`CodeRepo[]`（仅启用仓库）。

### PUT /repo/:repo_id/status 启用/停用

**请求**

```json
{ "status": 1 }
```

`status`：`1`=启用，`2`=停用。
**响应 `data`**：`null`。

### PUT /repo/:repo_id 编辑仓库

**请求**

```json
{ "repo_name": "order-service", "repo_url": "...", "default_branch": "main", "description": "..." }
```

（同注册，但无 `auth_token`；修改 `repo_name` 会改变克隆目录。）
**响应 `data`**：更新后的 `CodeRepo` 对象。

### PUT /repo/:repo_id/token 设置令牌

**请求**

```json
{ "auth_token": "glpat-xxxx" }
```

**响应 `data`**：`null`。

### DELETE /repo/:repo_id/token 清除令牌
**响应 `data`**：`null`。

---

## 代码解析任务

### POST /task/trigger 触发解析（CI 回调）

**请求**

```json
{ "task_id": "build-123", "repo_id": 1, "branch": "main" }
```

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| task_id | string | ✅ | 任务唯一标识（重复触发返回冲突） |
| repo_id | int | ✅ | 仓库 id |
| branch | string | ✅ | 代码分支（与默认分支 diff 后增量解析） |

**响应 `data`**：`TaskRecord` 对象（`task_id`、`repo_id`、`branch`、`status`、`create_time` 等）。

### GET /task/status?task_id=xx 任务状态
**响应 `data`**：`TaskRecord`（`status`：`pending`/`running`/`success`/`failed`，含错误信息、解析统计）。

### GET /task/list?repo_id=1&page=1&page_size=20 任务列表（分页）
**响应 `data`**：分页结构，`list` 为 `TaskRecord[]`，按时间倒序。

---

## 业务文档

### GET /doc/module/list?repo_id=1 业务模块列表
**响应 `data`**：`BusinessModule[]`（`id`、`module_name`、`repo_id` 等）。

### GET /doc/list?repo_id=1&module=&page=1&page_size=20 文档分页列表
**响应 `data`**：分页结构，`list` 为 `CodeFunctionDoc[]`。

### GET /doc/:doc_id 文档详情
**响应 `data`**：`CodeFunctionDoc` 完整对象，主要字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| id | int | 文档 id |
| repo_id | int | 仓库 id |
| module_name | string | 所属业务模块 |
| file_path | string | 源码文件路径 |
| func_name | string | 函数名 |
| func_line | int | 函数声明起始行号 |
| summary | string | 一句话业务摘要 |
| input_desc | string | 入参说明 |
| output_desc | string | 返回值说明 |
| process_flow | string | 业务执行流程 |
| rely_modules | string | 依赖模块 json 数组 |
| risk_point | string | 业务风险点 |
| content_source | int | 1=AI 自动生成 2=人工校正 |
| source_code_changed | int | 源码已更新待复核标记 |

### GET /doc/:doc_id/source 源码内容
**响应 `data`**：

```json
{
  "repo_id": 1, "repo_name": "order-service",
  "file_path": "internal/service/order.go",
  "func_name": "CreateOrder", "module_name": "order",
  "branch": "main", "content": "package main\n..."
}
```

### GET /doc/:doc_id/graph 函数调用图（D3）
**响应 `data`**：

```json
{
  "nodes": [{ "id": "order.CreateOrder", "module": "order", "func": "CreateOrder", "file": "...", "kind": "self", "depth": 0, "doc_id": 1 }],
  "links": [{ "source": "order.CreateOrder", "target": "user.GetUser" }]
}
```

### PUT /doc/:doc_id/edit 人工校正文档

**请求**

```json
{
  "summary": "...", "input_desc": "...", "output_desc": "...",
  "process_flow": "...", "risk_point": "...", "remark": "人工修正"
}
```

**响应 `data`**：`null`。

### POST /doc/:doc_id/reset 重置为原始 AI 版本
**响应 `data`**：`null`。

### GET /doc/modified/list?repo_id=1&page=&page_size= 人工校正文档列表
**响应 `data`**：分页结构。

### GET /doc/changelog?doc_id=1 迭代变更记录
**响应 `data`**：`CodeChangeLog[]`。

### GET /doc/:doc_id/history 修改历史（版本列表）
**响应 `data`**：`DocHistoryItem[]`（`log_id`、`operate_type` 1=编辑 2=重置、`operator`、`operate_time`、`remark`）。

### GET /doc/:doc_id/history/:log_id 历史快照详情
**响应 `data`**：

```json
{ "log_id": 1, "operate_type": 1, "operate_name": "编辑", "operator": "admin", "operate_time": "...", "remark": "", "before": {...}, "after": {...} }
```

`before`/`after` 为修改前后完整文档快照（原始 JSON）。

---

## 智能问答

### POST /chat/ask 多轮问答

**请求**

```json
{ "repo_id": 1, "session_id": "", "query": "下单模块的完整业务流程是什么？", "force_model": "", "force_high_quality": false }
```

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| repo_id | int | ✅ | 仓库 id（按库隔离检索） |
| query | string | ✅ | 用户输入 |
| session_id | string | — | 会话 id，为空则新建会话（带 Redis 会话记忆） |
| force_model | string | — | 强制指定模型 |
| force_high_quality | bool | — | 仅用高配模型 |

**响应 `data`**：

```json
{
  "session_id": "sess-xxx",
  "answer": "答案文本",
  "reference_list": [{ "doc_id": 1, "file_path": "...", "func_name": "CreateOrder", "func_line": 10, "module_name": "order" }],
  "used_model": "deepseek-chat",
  "cost": 0.0012,
  "history": [{ "role": "user", "content": "...", "ts": 1757000000 }]
}
```

### GET /chat/sessions?repo_id=1 会话列表
**响应 `data`**：`SessionMeta[]`（`session_id`、`repo_id`、`title`、`summary`、`created_at`、`updated_at`、`message_count`）。

### GET /chat/history?session_id=xx 会话历史
**响应 `data`**：`Message[]`（`role`：user/assistant，`content`，`ts`）。

### DELETE /chat/session?session_id=xx 删除会话
**响应 `data`**：`null`。

---

## 文档检索（RAG）

### POST /doc/search 跨模块 RAG 检索

**请求**

```json
{ "repo_id": 1, "query": "支付模块的核心逻辑", "module": "", "force_model": "", "force_high_quality": false }
```

**响应 `data`**：

```json
{
  "answer": "回答文本",
  "reference_list": [{ "doc_id": 1, "file_path": "...", "func_name": "...", "func_line": 10, "module_name": "order" }],
  "used_model": "deepseek-chat",
  "switch_count": 0,
  "cost": 0.0012
}
```

---

## 迭代影响分析

### POST /impact/analyze 影响分析（branch 模式）

**请求**

```json
{ "repo_id": 1, "branch": "feature/order-refactor", "direction": "both", "max_depth": 2, "version": "v1.1.0" }
```

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| repo_id | int | ✅ | 仓库 id |
| branch | string | — | 分支：自动 diff 推导变更函数（对比默认分支） |
| functions | array | — | 显式指定变更函数（与 branch 二选一） |
| query | string | — | 自然语言变更描述（RAG 定位，遗留字段） |
| session_id | string | — | 会话 id（多轮追问合并） |
| direction | string | — | `both`/`upstream`/`downstream`（默认 `both`） |
| max_depth | int | — | 传播深度（默认 2） |
| version | string | — | 迭代版本号（写入 `code_change_log.version`） |

**响应 `data`**：

```json
{
  "repo_id": 1,
  "changed": [{ "module": "order", "func": "CreateOrder", "file": "...", "depth": 0, "kind": 1, "edge": "", "summary": "..." }],
  "reverse_impact": [{ "module": "api", "func": "OrderHandler.Create", "depth": 1, "kind": 2, "edge": "order.CreateOrder <- api.OrderHandler.Create" }],
  "forward_impact": [{ "module": "user", "func": "GetUser", "depth": 1, "kind": 3, "edge": "order.CreateOrder -> user.GetUser" }],
  "design_doc": { "change_summary": "...", "business_impact": "...", "attention": "..." },
  "func_changes": [{ "module": "order", "func": "CreateOrder", "change_summary": "...", "business_impact": "...", "attention": "..." }],
  "api_schema": [{ "module": "order", "func": "CreateOrder", "file": "...", "change_type": "modified", "old": "...", "new": "..." }],
  "db_schema_changes": [{ "file": "...", "change_type": "alter", "tables": ["orders"], "affected_modules": ["order"] }],
  "test_files": [{ "file": "...", "funcs": ["CreateOrder"] }],
  "graph": { "nodes": [...], "links": [...] },
  "used_model": "deepseek-chat",
  "cost": 0.0021
}
```

`FuncRef` 字段：`module`、`func`、`file`、`depth`（0=直接修改）、`kind`（1 直接修改 / 2 上游 / 3 下游）、`edge`（传播边说明）、`summary`。
`APISchemaChange.change_type`：`added`/`modified`/`removed`。
`DBSchemaChange.change_type`：`create`/`alter`/`drop`/`rename`。

---

## 需求分析

### POST /requirement/analyze 新产品需求分析

**请求**

```json
{ "repo_id": 1, "user_requirement": "新增会员积分体系", "force_model": "", "force_high_quality": false }
```

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| repo_id | int | ✅ | 仓库 id |
| user_requirement | string | ✅ | 业务需求文本 |
| force_model | string | — | 强制指定模型 |
| force_high_quality | bool | — | 仅用高配模型 |

**响应 `data`**：

```json
{
  "related_modules": ["order", "user"],
  "related_functions": [{ "doc_id": 1, "file_path": "...", "func_name": "CreateOrder", "summary": "..." }],
  "analysis": "需求分析说明",
  "risk_points": ["风险1"],
  "suggestion": "开发建议",
  "knowledge_missing": false,
  "used_model": "deepseek-chat",
  "switch_count": 0,
  "cost": 0.0015
}
```

---

## 代码安全扫描

### POST /security/scan 全量扫描

**请求**

```json
{ "repo_id": 1 }
```

**响应 `data`**：

```json
{ "repo_id": 1, "scanned_files": 120, "total": 5, "high": 1, "medium": 2, "low": 2 }
```

### GET /security/list?repo_id=1&status=open&risk=high&page=1&page_size=20 发现列表

**响应 `data`**：`{ "list": [...], "total": 5, "page": 1, "page_size": 20 }`，`list` 为 `CodeSecretFinding[]`：

| 字段 | 类型 | 说明 |
|---|---|---|
| id | int | 发现 id |
| repo_id | int | 仓库 id |
| file_path | string | 文件路径 |
| line | int | 命中行号 |
| secret_type | string | 类型：aws_key/github_token/password/... |
| risk_level | string | high/medium/low |
| secret_value | string | 命中的敏感值（脱敏存储） |
| snippet | string | 所在行文本（脱敏） |
| recommend | string | 修复建议 |
| status | string | open/fixed/false_positive |

### PUT /security/:id/status 更新状态

**请求**

```json
{ "status": "false_positive" }
```

`status`：`open`/`fixed`/`false_positive`。
**响应 `data`**：`null`。

---

## 模块依赖图谱

### GET /relation/list?repo_id=1 模块上下游依赖
**响应 `data`**：依赖关系列表。

### POST /relation/add 新增依赖

**请求**

```json
{ "repo_id": 1, "src_module": "order", "dst_module": "user" }
```

**响应 `data`**：`null`。

### DELETE /relation 删除依赖
**请求**（query）：`repo_id`、`src_module`、`dst_module`。
**响应 `data`**：`null`。

---

## 统计

### GET /report/basic?repo_id=1 基础统计
**响应 `data`**：

```json
{
  "total_doc_count": 100, "manual_doc_count": 3, "auto_doc_count": 97,
  "pending_review_count": 1, "module_count": 10
}
```

---

## 其它

### GET /health 健康检查（免鉴权）
**响应 `data`**：

```json
{ "version": "0.1.6", "mysql": "ok", "llm_service": "ok", "status": "running" }
```

### POST /webhook/git_push GitLab/Gitee 分支推送回调（免鉴权，自有签名）
GitLab 分支 push webhook 回调，自动触发对应仓库的代码解析任务。
