<div align="center">

<img src="ai-code-wiki/web/public/logo.svg" width="28" height="28" align="left"> # AI·CODE WIKI

**AI 驱动的代码知识库：代码 → 业务文档 → 智能问答 / 影响分析 / 安全扫描**

基于 CI 增量解析，把仓库代码自动沉淀为函数级业务文档知识库，支撑跨模块 RAG 问答、迭代影响分析、需求分析与代码安全扫描。

![Go](https://img.shields.io/badge/Go-1.22-00ADD8?logo=go)
![Vue3](https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs)
![MySQL](https://img.shields.io/badge/MySQL-8-4479a1?logo=mysql)
![Chroma](https://img.shields.io/badge/Vector-Chroma/Milvus-9b59b6)
![Docker](https://img.shields.io/badge/Docker-Compose-2496ed?logo=docker)
![Version](https://img.shields.io/badge/version-0.1.6-4dabf7)
![License](https://img.shields.io/badge/License-MIT-green)

</div>

---

## ✨ 核心特性

| | 特性 | 说明 |
|---|---|---|
| 📚 | **代码 → 业务文档** | CI 触发增量解析（Go/PHP），AST 提取函数级信息，LLM 生成标准化业务文档（摘要/入参/出参/流程/风险），沉淀模块依赖图谱与函数级调用边 |
| 💬 | **多轮智能问答** | RAG 检索 + Redis 会话记忆（滑动窗口 + 滚动摘要），支持连续追问 |
| 🎯 | **混合检索** | 向量语义召回 ∪ 关键词精确召回，答案带 `文件:行号` 引用，一键跳转源码定位 |
| 🔍 | **迭代影响分析** | 函数级调用边双向 BFS 传播（上游/下游），叠加 **API 签名变更检测 / 数据库表结构变更 / 回归测试圈定**，LLM 合成变更说明与影响分析并沉淀变更日志 |
| 🕸️ | **函数调用图** | 文档详情与影响分析内置 D3 力导向调用图，节点跳转源码 |
| 🛡️ | **代码安全扫描** | 正则引擎检测硬编码密钥/密码/令牌/私钥（12 类规则），脱敏落库，支持全量/增量扫描与误报/修复管理 |
| 📈 | **成本控制** | 源码未变更函数缓存命中跳过 LLM 生成（日志输出命中率），单任务调用预算上限 |
| 🧹 | **幽灵文档清理** | 函数/文件从代码删除后，对应文档自动下线（逻辑删除 + 审计日志 + 向量清理） |
| 🗃️ | **多仓库管理** | 仓库注册（自动校验可用性）/ 编辑 / 启停 / 令牌设置与清除，私有仓库 HTTPS Bearer 鉴权（AES-GCM 加密存储、出参脱敏） |
| ✍️ | **人工校正** | 文档人工编辑/重置（保留 AI 原始版本）、版本历史、操作日志、迭代变更记录 |

## 📸 界面预览

| 登录页 | 文档列表 | 智能问答 |
|---|---|---|
| ![登录页](docs/screenshots/login.png) | ![文档列表](docs/screenshots/docs.png) | ![智能问答](docs/screenshots/chat.png) |

| 迭代影响 | 代码安全扫描 | 任务管理 |
|---|---|---|
| ![迭代影响](docs/screenshots/impact.png) | ![安全扫描](docs/screenshots/security.png) | ![任务管理](docs/screenshots/tasks.png) |

| 仓库管理 |
|---|
| ![仓库管理](docs/screenshots/repos.png) |

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

Docker Compose 一键启动（MySQL / Chroma / Redis / LLM 微服务 / API）：

```bash
docker compose up -d
# 默认管理员：admin / admin123（生产务必通过 AUTH_ADMIN_PASSWORD 修改）
```

典型使用流程（具体接口参数见 [API 参考](docs/API.md)）：

```text
1. 注册仓库   → POST /repo/register
2. 触发解析   → POST /task/trigger  （解析出函数级业务文档）
3. 智能问答   → POST /chat/ask      （RAG 检索 + 会话记忆）
4. 迭代影响   → POST /impact/analyze（branch 自动 diff → 影响点 + 变更说明）
5. 安全扫描   → POST /security/scan （硬编码密钥/密码检测）
```

## 📁 目录结构

```
├── ai-code-wiki/            # 主项目
│   ├── internal/            # Go 后端（handler / service / repo / model / middleware / router）
│   ├── pkg/                 # 公共包（astgo / astphp / vector / secretscan / git / taskqueue ...）
│   └── web/                 # Vue3 + Vite 前端（views / components / store / router）
├── ai-wiki-llm/             # Python LLM 微服务（多模型调度 / embedding）
├── init_sql/                # 数据库初始化（init.sql 全量 + migrations/ 增量迁移）
├── docs/                    # API 文档与界面截图
└── docker-compose.yml       # 一键编排
```

## 📄 文档

- [API 参考](docs/API.md)：全部接口的参数与返回值定义
- [完整项目文档](ai-code-wiki/README.md)：环境变量、表结构、生产部署注意事项
- [架构设计](ai-code-wiki/docs/architecture.md)

## 🧩 近期迭代

- 仓库注册/编辑自动校验可用性（`git ls-remote`）、仓库行内编辑/令牌设置、会话删除
- 需求分析 JSON 解析健壮性（花括号配平提取 + 失败重试 + 诊断日志）
- 代码安全扫描、增量向量精度（函数级全文 + 向量侧过滤）
- 函数调用图可视化（D3）、LLM 成本控制、影响分析增强（API 签名/表结构/回归测试）
- 幽灵文档清理、混合检索、问答引用定位（`文件:行号` 跳转）
- 私有仓库 HTTPS Token 鉴权、SPA 化前端重构
- 登录鉴权、多轮智能问答、迭代影响分析（MVP）

## 🗺️ 迭代规划

### P0 · 稳定性补齐
- **多模型降级落地**：启用 qwen-turbo/plus 等备用模型（配置 DASHSCOPE key），让熔断降级真正可用（目前仅 deepseek 启用，无模型可切）
- **熔断/降级状态可视化**：Python 暴露各模型运行状态（熔断中 / 限流中 / 正常 / 降级次数），Go 转发，前端在「模型与用量」页展示
- **影响分析降级**：LLM 不可用时影响点/调用图照常返回，变更说明标记「AI 不可用」，不再整体失败

### P1 · 集成与自动化
- **MR/PR 机器人**：GitLab/GitHub 合并请求回调自动跑影响分析 + 安全扫描，结果评论到 MR
- **通知推送**：任务完成 / 安全高危发现 → 钉钉 / 企微 webhook

### P2 · 知识库增强
- **多语言解析**：扩展 Java / Python / TypeScript（当前 Go / PHP）
- **检索重排 + 语义缓存**：cross-encoder 重排提升问答准确率，相似问题语义缓存降本
- **模型用量页增强**：成功率 / 失败原因（401/402/429/超时）/ 熔断次数统计

## License

MIT
