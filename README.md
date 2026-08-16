<div align="center">

# AI·CODE WIKI

**AI 驱动的代码知识库：代码 → 业务文档 → 智能问答 / 影响分析 / 安全扫描**

基于 CI 增量解析，把仓库代码自动沉淀为函数级业务文档知识库，支撑跨模块 RAG 问答、迭代影响分析、需求分析与代码安全扫描。

![Go](https://img.shields.io/badge/Go-1.22-00ADD8?logo=go)
![Vue3](https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs)
![MySQL](https://img.shields.io/badge/MySQL-8-4479a1?logo=mysql)
![Chroma](https://img.shields.io/badge/Vector-Chroma/Milvus-9b59b6)
![Docker](https://img.shields.io/badge/Docker-Compose-2496ed?logo=docker)
![License](https://img.shields.io/badge/License-MIT-green)

</div>

---

## ✨ 核心特性

| | 特性 | 说明 |
|---|---|---|
| 📚 | **代码 → 业务文档** | CI 触发增量解析（Go/PHP），AST 提取函数级信息，LLM 生成标准化业务文档（摘要/入参/出参/流程/风险），沉淀模块依赖图谱与函数级调用边 |
| 💬 | **多轮智能问答** | RAG 检索 + Redis 会话记忆（滑动窗口 + 滚动摘要），支持连续追问 |
| 🎯 | **混合检索** | 向量语义召回 ∪ 关键词精确召回，答案带 `文件:行号` 引用，一键跳转源码定位 |
| 🔍 | **迭代影响分析** | 函数级调用边双向 BFS 传播（上游/下游），叠加 **API 签名变更检测 / 数据库表结构变更 / 回归测试圈定**，LLM 合成设计文档初稿并沉淀变更日志 |
| 🕸️ | **函数调用图** | 文档详情与影响分析内置 D3 力导向调用图，节点跳转源码 |
| 🛡️ | **代码安全扫描** | 正则引擎检测硬编码密钥/密码/令牌/私钥（11 类规则），脱敏落库，支持全量/增量扫描与误报/修复管理 |
| 📈 | **成本控制** | 源码未变更函数缓存命中跳过 LLM 生成（日志输出命中率），单任务调用预算上限 |
| 🧹 | **幽灵文档清理** | 函数/文件从代码删除后，对应文档自动下线（逻辑删除 + 审计日志 + 向量清理） |
| 🗃️ | **多仓库管理** | 仓库注册/启停/令牌管理，私有仓库 HTTPS Bearer 鉴权（AES-GCM 加密存储、出参脱敏） |
| ✍️ | **人工校正** | 文档人工编辑/重置（保留 AI 原始版本）、版本历史、操作日志、迭代变更记录 |

## 📸 界面预览

| 文档列表 | 智能问答 | 迭代影响 |
|---|---|---|
| ![文档列表](docs/screenshots/docs.png) | ![智能问答](docs/screenshots/chat.png) | ![迭代影响](docs/screenshots/impact.png) |

| 代码安全扫描 | 任务管理 | 仓库管理 |
|---|---|---|
| ![安全扫描](docs/screenshots/security.png) | ![任务管理](docs/screenshots/tasks.png) | ![仓库管理](docs/screenshots/repos.png) |

## 🏗️ 架构概览

```
┌─────────────┐  触发    ┌──────────────────┐  git diff  ┌────────────┐
│   CI / 手动  │ ──────▶ │    ai-code-wiki  │ ─────────▶ │  Git 仓库   │
└─────────────┘          │   (Go + Gin)     │            └────────────┘
                         └────────┬─────────┘
                                  │ AST 解析 → LLM 生成文档
                       ┌──────────┼──────────────────────────┐
                       ▼          ▼                          ▼
                 ┌──────────┐ ┌──────────┐          ┌──────────────┐
                 │  MySQL   │ │ Chroma / │          │ ai-wiki-llm  │
                 │ 业务文档  │ │ Milvus   │          │ (Python)     │
                 │ 调用边/图谱│ │ 函数向量  │          │ 多模型调度/embedding │
                 └──────────┘ └──────────┘          └──────────────┘
                       ▲          ▲                          ▲
                       └──── Redis（会话记忆）/ RabbitMQ（异步任务队列）
```

- **后端**：Go + Gin + GORM，异步任务队列（内存/RabbitMQ），REST API
- **前端**：Vue3 + Vite SPA（vue-router history 路由）
- **向量库**：Chroma（默认）/ Milvus（可切换），函数级向量 + 仓库/模块侧过滤
- **LLM 微服务**：Python 多模型调度器（低价优先、失败降级、熔断限流），模型池 YAML 配置
- **会话记忆**：Redis（滑动窗口 + 滚动摘要）

## 🚀 快速开始

```bash
# 1. 注册仓库（私有仓库可携带访问令牌）
POST /api/v1/repo/register
{"repo_name":"order-service","repo_url":"https://gitlab.com/group/order.git","auth_token":"glpat-xxxx"}

# 2. 触发解析（CI 回调或手动）
POST /api/v1/task/trigger
{"repo_id":1,"task_id":"build-123","branch":"main"}

# 3. 智能问答
POST /api/v1/chat/ask
{"repo_id":1,"query":"下单模块的完整业务流程是什么？"}

# 4. 迭代影响分析
POST /api/v1/impact/analyze
{"repo_id":1,"branch":"feature/order-refactor","direction":"both","max_depth":2}

# 5. 代码安全扫描
POST /api/v1/security/scan
{"repo_id":1}
```

Docker Compose 一键启动（MySQL / Chroma / Redis / LLM 微服务 / API）：

```bash
docker compose up -d
# 默认管理员：admin / admin123（生产务必通过 AUTH_ADMIN_PASSWORD 修改）
```

## 📁 目录结构

```
├── ai-code-wiki/            # 主项目
│   ├── internal/            # Go 后端（handler / service / repo / model / middleware / router）
│   ├── pkg/                 # 公共包（astgo / astphp / vector / secretscan / git / taskqueue ...）
│   ├── web/                 # Vue3 + Vite 前端（views / components / store / router）
│   ├── config/              # 配置文件
│   └── init_sql/            # 数据库初始化与增量迁移
├── ai-wiki-llm/             # Python LLM 微服务（多模型调度 / embedding）
├── docs/screenshots/        # 界面截图
└── docker-compose.yml       # 一键编排
```

## 📄 文档

- [完整项目文档](ai-code-wiki/README.md)：环境变量、API 清单、表结构、生产部署注意事项
- [架构设计](ai-code-wiki/docs/architecture.md)

## 🔧 主要 API

| 模块 | 接口 |
|---|---|
| 仓库 | `POST /api/v1/repo/register` · `GET /api/v1/repo/list` · `PUT /api/v1/repo/:id/status` · `DELETE /api/v1/repo/:id/token` |
| 任务 | `POST /api/v1/task/trigger` · `GET /api/v1/task/status` · `GET /api/v1/task/list` |
| 文档 | `POST /api/v1/doc/search` · `GET /api/v1/doc/list` · `GET /api/v1/doc/:id` · `GET /api/v1/doc/:id/source` · `GET /api/v1/doc/:id/graph` · `PUT /api/v1/doc/:id/edit` |
| 影响分析 | `POST /api/v1/impact/analyze` |
| 问答 | `POST /api/v1/chat/ask` · `GET /api/v1/chat/sessions` · `GET /api/v1/chat/history` |
| 需求分析 | `POST /api/v1/requirement/analyze` |
| 安全扫描 | `POST /api/v1/security/scan` · `GET /api/v1/security/list` · `PUT /api/v1/security/:id/status` |
| 统计 | `GET /api/v1/report/basic` |

## 🧩 近期迭代

- **V2**：代码安全扫描、增量向量精度（函数级全文 + 向量侧过滤）
- **V2**：函数调用图可视化（D3）、LLM 成本控制、影响分析增强（API 签名/表结构/回归测试）
- **V2**：幽灵文档清理、混合检索、问答引用定位（`文件:行号` 跳转）
- **V2**：私有仓库 HTTPS Token 鉴权、SPA 化前端重构
- **V1**：登录鉴权、多轮智能问答、迭代影响分析（MVP）

## License

MIT
