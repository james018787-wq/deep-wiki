# ai-code-wiki

AI 代码知识库系统。通过 CI 触发的代码解析任务，自动提取仓库内 Go / PHP 源码中的函数级信息，调用大模型生成标准化业务文档，并沉淀模块依赖知识图谱，支撑跨模块 RAG 检索与新产品需求分析。

## 1. 项目介绍与功能清单

### 1.1 架构概览

```
┌─────────────┐   触发     ┌──────────────────┐   git diff   ┌────────────┐
│     CI      │──────────▶ │  ai-code-wiki    │─────────────▶│   Git仓库   │
└─────────────┘            │  (Go + Gin)      │              └────────────┘
                           │                  │  go/php源码
                           │  任务流水线       │─────────────▶  解析提取函数
                           │  文档管理/检索    │─────────────▶  LLM微服务生成文档
                           │  模块依赖图谱     │
                           │  需求分析         │
                           └────────┬─────────┘
                                    │ 写入/读取
                          ┌─────────▼─────────┐   ┌─────────────┐
                          │  MySQL 8.0         │   │  Chroma     │
                          └───────────────────┘   │  向量库      │
                          ┌───────────────────┐   └─────────────┘
                          │  ai-wiki-llm       │◀────────┬────────
                          │  (Python FastAPI)  │  embedding/chat
                          └───────────────────┘
```

### 1.2 功能清单

| 模块 | 功能 | 说明 |
| ---- | ---- | ---- |
| 代码解析任务 | 任务触发 / 状态查询 / 列表 | CI 回调触发，后台异步执行 |
| 文档解析 | Go AST 解析 | `pkg/astgo`，提取函数名、源码、调用关系 |
| | PHP 简易解析 | `pkg/astphp`，正则提取函数（MVP，无重型解析库） |
| 业务文档 | 检索 / 详情 / 人工校正 / 重置 / 变更记录 | 校正带修改前后快照日志，`origin_auto_doc` 永久保留 |
| 模块依赖图谱 | 上下游查询 / 人工新增 / 删除 | AST 识别 ∪ 人工关系，操作带日志 |
| RAG 检索 | 自然语言跨模块检索 | Chroma 向量召回 + 模块依赖扩充 |
| 需求分析 | 需求 → 结构化分析 | 复用检索流水线，LLM 输出 JSON |
| 平台能力 | API 密钥鉴权 / request_id 日志 / 健康检查 | MVP 单密钥，统一日志工具 |
| 异步任务 | 任务队列抽象接口 | 当前 goroutine 本地执行，预留 MQ 扩展 |

### 1.3 技术栈

- 后端：Go 1.22 + Gin + GORM（MySQL 8.0）
- LLM 微服务：Python 3.11 + FastAPI + LangChain（`ai-wiki-llm`）
- 向量库：Chroma（HTTP 查询），`pkg/vector` 预留 Milvus/Redis/Faiss 扩展
- 部署：Docker Compose（mysql / chroma / Go API / Python LLM）

## 2. 本地开发启动

### 2.1 目录结构

```
ai-code-wiki/
├── cmd/main.go              # 服务入口
├── config/config.yaml       # 基础配置
├── internal/
│   ├── config/              # 配置加载与环境变量覆盖
│   ├── handler/             # HTTP 层（参数校验 + 统一响应）
│   ├── middleware/          # 请求追踪 / 异常恢复 / API鉴权
│   ├── model/               # GORM 模型
│   ├── repo/                # 数据访问层
│   ├── router/              # 路由注册
│   └── service/             # 业务逻辑层
├── pkg/
│   ├── astgo/               # Go 源码 AST 解析
│   ├── astphp/              # PHP 源码简易解析
│   ├── git/                 # git 命令封装
│   ├── logger/              # 统一日志工具
│   ├── llm/                 # LLM 服务调用
│   ├── taskqueue/           # 异步任务队列抽象
│   └── vector/              # 向量库通用接口
├── init_sql/init.sql        # 建表脚本（docker 自动导入）
├── docker-compose.yml
└── Dockerfile
```

### 2.2 本地开发（不依赖 Docker）

```bash
# 1. 准备 MySQL8.0，导入 init_sql/init.sql，建库 ai_code_wiki
# 2. 准备 Python LLM 微服务（ai-wiki-llm，独立启动）
# 3. 准备 Chroma 向量库（http://localhost:8000）

# 4. 修改 config/config.yaml（mysql 密码、llm.base_url、git.repo_url 等）
# 5. 启动 Go 服务
cd ai-code-wiki
GOPROXY=https://goproxy.cn,direct go run ./cmd
# 默认监听 :8080，健康检查：curl http://localhost:8080/health
```

> 国内网络拉取 Go 依赖建议设置 `GOPROXY=https://goproxy.cn,direct`。

### 2.3 Docker Compose 一键启动

```bash
# 在项目根目录（含 docker-compose.yml 的目录）
docker compose up -d
# 等待所有服务健康后查看状态
docker compose ps
```

Compose 将启动 4 个服务：

| 服务 | 端口 | 说明 |
| ---- | ---- | ---- |
| mysql | 3306 | 数据库，首次启动自动导入 init_sql |
| chroma | 8000 | 向量库 |
| ai-code-wiki-api | 8080 | Go 后端（带 healthcheck） |
| ai-wiki-llm | 9000 | Python LLM 微服务 |

```bash
# 验证
curl http://localhost:8080/health
# {"mysql":"ok","llm_service":"ok","status":"running"}

# 停止 / 清理数据
docker compose down
docker compose down -v   # 连数据卷一起删除（慎用）
```

## 3. 环境变量说明

配置优先级：环境变量 > `config/config.yaml`。所有环境变量均为可选（未设置时回退到 yaml 默认值）。

| 环境变量 | 对应配置 | 默认值 | 说明 |
| -------- | -------- | ------ | ---- |
| `SERVER_PORT` | server.port | 8080 | HTTP 监听端口 |
| `SERVER_MODE` | server.mode | debug | gin 模式：debug / release / test |
| `DB_HOST` | mysql.host | 127.0.0.1 | MySQL 地址 |
| `DB_PORT` | mysql.port | 3306 | MySQL 端口 |
| `DB_USER` | mysql.user | root | MySQL 用户 |
| `DB_PASSWORD` | mysql.password | root | MySQL 密码 |
| `DB_NAME` | mysql.database | ai_code_wiki | 数据库名 |
| `CHROMA_URL` | vector.host/port | http://chroma:8000 | Chroma 向量库地址（解析为 host+port，`VECTOR_DRIVER=chroma` 时生效） |
| `VECTOR_DRIVER` | vector.engine | chroma | 向量引擎驱动：`chroma` / `milvus`，业务代码经抽象接口不感知底层引擎 |
| `MILVUS_HOST` | vector.host | 空 | Milvus 服务地址（如 `127.0.0.1` 或 docker-compose 服务名 `milvus`） |
| `MILVUS_PORT` | vector.port | 19530 | Milvus 端口 |
| `MILVUS_COLLECTION` | vector.collection | code_doc | Milvus 集合名 |
| `MILVUS_DIM` | vector.dim | 1536 | embedding 向量维度，需与向量化服务输出一致（如 OpenAI text-embedding-3-small 为 1536） |
| `MILVUS_USER` | vector.user | 空 | Milvus 用户名（可选，服务端开启鉴权时必填） |
| `MILVUS_PASSWORD` | vector.password | 空 | Milvus 密码（可选） |
| `LLM_SERVICE_URL` | llm.base_url | http://ai-wiki-llm:9000 | Python LLM 微服务地址 |
| `GIT_CLONE_DIR` | git.clone_dir | ./repo_cache | 代码仓库本地克隆目录 |
| `API_SECRET_KEY` | —（鉴权中间件直接读取） | 空 | API 密钥，为空时 `/api/v1` 鉴权关闭；非空时请求需带请求头 `X-Api-Secret` |
| `WEBHOOK_SECRET` | —（webhook 处理器直接读取） | 空 | webhook 签名密钥，为空时跳过签名校验（开发环境）；非空时 GitLab/Gitee 回调需携带对应 Token/签名，校验失败返回 403 |
| `TASK_QUEUE_DRIVER` | task_queue.driver | memory | 异步任务队列驱动：`memory`（开发，内存 channel）/ `rabbitmq`（生产，消息持久化 + 手动 ACK） |
| `RABBITMQ_URL` | task_queue.rabbitmq_url | 空 | RabbitMQ 连接地址，如 `amqp://user:pass@host:5672/`（`TASK_QUEUE_DRIVER=rabbitmq` 时必填） |
| `TASK_QUEUE_NAME` | task_queue.queue_name | ai-code-wiki-task | 任务队列名（RabbitMQ 下自动创建持久化队列） |
| `TASK_QUEUE_MAX_RETRY` | task_queue.max_retry | 3 | 任务最大重试次数，消费失败自动重投，超过上限标记任务失败 |
| `TASK_QUEUE_CONCURRENCY` | task_queue.concurrency | 2 | 任务消费协程数（内存与 RabbitMQ 均生效） |
| `FILTER_IGNORE_DIRS` | filter.ignore_dirs | vendor,node_modules,mock,fixture | 解析流水线忽略的目录名（逗号分隔，路径任意层级命中即跳过文件，如第三方依赖/测试数据目录） |
| `FILTER_IGNORE_FILE_REGEX` | filter.ignore_file_re | `_test\.go$` | 解析流水线忽略的文件正则（逗号分隔，匹配相对路径，如 Go 测试文件 `*_test.go`） |
| `FILTER_ALLOW_EXTS` | filter.allow_exts | go,php | 解析流水线允许解析的代码文件后缀（逗号分隔，不含点；非业务代码后缀直接跳过） |

> 说明：
> - `API_SECRET_KEY` 由 `internal/middleware` 直接读取，不经过 config.yaml。
> - Docker Compose 中 Go 服务已注入 `DB_*`、`CHROMA_URL`、`LLM_SERVICE_URL`，生产部署请补充 `API_SECRET_KEY`。
> - `llm.provider/api_key/model` 为预留配置，当前版本未启用。
> - 向量引擎选择：默认 `chroma`；切换 Milvus 时设置 `VECTOR_DRIVER=milvus` 并配置 `MILVUS_*`，集合不存在会自动创建（FLAT 索引 + L2 距离）。初始化失败时向量同步/检索降级（跳过或返回"未配置"），不影响服务启动。

## 4. API 简要说明

统一返回格式：`{"code":0,"msg":"ok","data":...}`；失败时 `code` 非 0。
`/api/v1` 分组下的接口受 API 密钥保护（配置了 `API_SECRET_KEY` 时需带请求头 `X-Api-Secret`）。

| 方法 | 路径 | 说明 |
| ---- | ---- | ---- |
| GET | `/health` | 健康检查（跳过鉴权），探测 mysql / llm 连通性 |
| GET | `/` | 首页，302 跳转 `/webstatic/docs.html`（极简前端入口） |
| POST | `/api/v1/webhook/git_push` | GitLab/Gitee 分支 push 回调，自动触发解析任务（跳过 X-Api-Secret，使用 WEBHOOK_SECRET 签名鉴权） |
| POST | `/api/v1/task/trigger` | 触发代码解析任务（CI 回调），body: `{task_id, branch}` |
| GET | `/api/v1/task/status?task_id=xxx` | 查询任务状态 |
| GET | `/api/v1/task/list?page=1&page_size=20` | 任务列表（分页，时间倒序） |
| POST | `/api/v1/doc/search` | 自然语言跨模块检索，body: `{query, module?}` |
| GET | `/api/v1/doc/module/list` | 获取所有业务模块 |
| GET | `/api/v1/doc/list?module=xxx&page=1&page_size=20` | 分页查询函数文档列表（前端列表页使用） |
| GET | `/api/v1/doc/:doc_id` | 获取文档详情 |
| PUT | `/api/v1/doc/:doc_id/edit` | 人工校正文档（记录修改前后快照） |
| POST | `/api/v1/doc/:doc_id/reset` | 重置为原始 AI 版本 |
| GET | `/api/v1/doc/modified/list?page=1&page_size=20` | 人工校正文档列表 |
| GET | `/api/v1/doc/changelog?doc_id=xxx` | 文档迭代变更记录 |
| GET | `/api/v1/relation/list?module=xxx&direction=out|in` | 模块上下游依赖 |
| POST | `/api/v1/relation/add` | 人工新增依赖（写操作日志） |
| DELETE | `/api/v1/relation` | 删除依赖（逻辑删除 + 操作日志） |
| POST | `/api/v1/requirement/analyze` | 需求分析，body: `{user_requirement}` |
| GET | `/api/v1/report/basic` | 基础统计：总文档数 / 人工校正数 / 自动生成数 / 待复核数 / 模块总数 |

### 4.2 极简前端

`./webstatic` 提供原生 HTML + Vue3 CDN 页面（无构建），由后端 `/webstatic` 静态路由挂载：

- `docs.html` 文档列表（分页 + 模块筛选，点击进入编辑）
- `doc-edit.html` 文档编辑/详情（加载现有文档，`PUT /api/v1/doc/:doc_id/edit` 提交、支持重置）
- `tasks.html` 任务管理（任务列表 / 状态查询 / 触发解析任务）

鉴权密钥在 `webstatic/config.js` 的 `apiSecret` 配置（与后端 `API_SECRET_KEY` 一致）。

### 4.1 典型调用示例

```bash
# 触发任务（本地开发未启用鉴权）
curl -X POST http://localhost:8080/api/v1/task/trigger \
  -H 'Content-Type: application/json' \
  -d '{"task_id":"build-001","branch":"feature/x"}'

# 查询任务状态
curl 'http://localhost:8080/api/v1/task/status?task_id=build-001'

# RAG 检索
curl -X POST http://localhost:8080/api/v1/doc/search \
  -H 'Content-Type: application/json' \
  -d '{"query":"下单支付流程"}'

# 生产环境启用鉴权后需带密钥
curl -X POST http://localhost:8080/api/v1/doc/search \
  -H 'Content-Type: application/json' -H 'X-Api-Secret: your-secret' \
  -d '{"query":"下单支付流程"}'
```

## 5. 开发顺序与 MVP 已知限制

### 5.1 建议开发顺序

1. 基础框架：配置加载、MySQL 接入、统一响应/错误、统一日志
2. 代码解析任务：git 拉取 → diff → 源码解析 → LLM 生成文档落库
3. 文档检索与人工校正：RAG 检索、编辑/重置（快照日志）、向量同步
4. 模块依赖图谱：AST 识别 + 人工关系 + 操作日志
5. 需求分析：复用检索流水线，结构化 LLM 输出

### 5.2 MVP 已知限制

- **PHP 解析**：基于正则的简易实现，不做深度 AST，可能误匹配（注释/字符串规避已处理，匿名函数/泛型等特殊场景见 `pkg/astphp` 注释）。
- **向量引擎**：默认 Chroma/Milvus 已实现（`VECTOR_DRIVER` 切换），Redis/Faiss 未实现。
- **LLM 依赖**：文档生成、检索回答、需求分析均依赖外部大模型服务，未内置模型。
- **鉴权**：仅单密钥（`API_SECRET_KEY`），无 RBAC、无用户体系。
- **任务队列**：已抽象 `pkg/taskqueue` 接口（`SubmitTask`/`ConsumeTask`），默认内存队列，生产可切换 RabbitMQ（`TASK_QUEUE_DRIVER=rabbitmq`）；消费失败自动重试，超过上限标记任务失败。
- **日志**：仅控制台输出，文件输出已留扩展点（`logger.NewFileSink` / `logger.SetOutput`），未启用。
- **人工删除依赖与 AST 重加**：人工删除的关系在后续自动任务中"不被 AST 重新添加"的标记逻辑待完善。
- **源码变更复核**：`source_code_changed=1` 的待复核流程仅有标记，复核页面/接口未实现。
- **跨模块 RAG**：召回扩充基于模块依赖表，未做复杂权重/排序调优。

## 6. 生产部署注意事项

1. **修改默认密码**
   - `docker-compose.yml` 中 `MYSQL_ROOT_PASSWORD`（当前 `Wiki@2026`）、Go 服务 `DB_PASSWORD` 必须改为强密码并保持一致。
2. **禁止公网直接暴露**
   - 服务仅应监听内网，通过 Nginx/网关反向代理对外；`/api/v1` 务必配置 `API_SECRET_KEY`（环境变量注入），并限制来源 IP。
3. **配置 API 密钥**
   - 为 Go 服务注入 `API_SECRET_KEY`，同时为 `ai-wiki-llm` 配置真实的 `OPENAI_API_KEY` / `OPENAI_BASE_URL` / `LLM_MODEL`。
4. **向量库后期切换 Milvus**
   - `pkg/vector` 已抽象 `Client` 接口（Upsert/Delete/Search）与 `vector.engine` 配置项；
   - 切换到 Milvus 时实现 `Client` 接口并在 `New()` 中按 engine 分发，上层 `search_service` / `doc_service` 无需改动。
5. **替换 MQ（生产化异步任务）**
   - `pkg/taskqueue.TaskQueue` 提供 `SubmitTask`/`ConsumeTask` 抽象接口，已实现 `memory`（开发，内存 channel）与 `rabbitmq`（生产，amqp091-go，消息持久化 + 手动 ACK）两种实现；
   - 通过 `TASK_QUEUE_DRIVER` 环境变量切换，配合 `RABBITMQ_URL` / `TASK_QUEUE_NAME` / `TASK_QUEUE_MAX_RETRY` / `TASK_QUEUE_CONCURRENCY`；
   - 解析任务 / 向量更新任务统一投递到队列，由独立后台消费协程执行，失败自动重投（`retry_count` 记录），超过最大重试次数标记任务失败。
6. **数据安全与备份**
   - 定期备份 MySQL 数据卷；开启 `restart: always` 保障自愈；健康检查失败会触发容器重启，需保证 MySQL/LLM 先行就绪（`depends_on: service_healthy`）。
7. **日志与监控**
   - 生产建议启用文件日志（`logger.NewFileSink`），统一采集到日志平台；健康检查 `/health` 已适配 docker healthcheck（依赖不可用返回 503）。

## 7. 表结构简要说明

所有表均为 InnoDB、utf8mb4，逻辑删除统一用 `is_deleted`（0 未删 / 1 已删）。

### 7.1 business_module 业务模块表

| 字段 | 说明 |
| ---- | ---- |
| id | 主键 |
| module_name | 业务模块名称（唯一） |
| desc | 模块说明 |
| create_time / update_time | 时间戳 |
| is_deleted | 逻辑删除 |

### 7.2 code_function_doc 函数业务文档主表（核心）

最小切片单元为单个函数，一条记录对应一个函数。

| 字段 | 说明 |
| ---- | ---- |
| id | 主键 |
| module_name / file_path / func_name | 所属模块 / 文件路径 / 函数名（file_path+func_name+is_deleted 唯一） |
| source_code | 函数源码片段 |
| summary / input_desc / output_desc / process_flow / rely_modules / risk_point | AI 生成的标准化业务文档字段 |
| origin_auto_doc | **原始 AI 自动生成文档，永久保存，人工校正/重置均不可覆盖** |
| content_source | 1=AI自动生成 2=人工校正 |
| source_code_changed | 源码已更新、文档待复核标记 |
| last_edit_user / last_edit_time | 最后人工校正信息 |

### 7.3 module_relation 模块依赖知识图谱表

| 字段 | 说明 |
| ---- | ---- |
| source_module / target_module | 源模块 / 被依赖模块 |
| relation_type | 1=同步调用 2=异步MQ事件 |
| source | 1=AST自动识别 2=人工手动添加 |
| creator / remark | 创建人 / 备注 |

### 7.4 doc_modify_log 文档人工校正日志

编辑/重置时记录修改前后完整文档 JSON 快照（before_content / after_content），`operate_type`：1=编辑 2=重置。

### 7.5 relation_modify_log 模块依赖关系操作日志

`operate_type`：1=新增 2=编辑 3=删除，记录 operator / remark。

### 7.6 code_change_log 代码迭代变更历史记录

关联文档的版本变更摘要、业务影响范围、上线注意事项。

### 7.7 task_record 代码解析任务记录表

| 字段 | 说明 |
| ---- | ---- |
| task_id | 任务唯一标识（唯一） |
| branch | 代码分支 |
| status | 0=待执行 1=执行中 2=成功 3=失败 |
| retry_count | 失败重试次数（队列消费失败重新投递时自增，状态置回待执行） |
| err_msg | 错误信息 |
| finish_time | 完成时间 |