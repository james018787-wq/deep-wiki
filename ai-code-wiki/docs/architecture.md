# AI-Code-Wiki 系统架构文档

> 梳理整个系统的组件、数据流与「模型交互」边界，回答：
> **为什么模型交互要统一收口在 Python（ai-wiki-llm）？**
> 对应代码版本：master @ 多模型调度收口 Python 后。

## 一、总体架构

```
                    ┌────────────┐    git push 回调
    CI / GitLab ───▶│  ai-code-wiki (Go + Gin)   │◀────────────── 用户前端 (webstatic)
                    │                            │   /api/v1/*
                    └───────┬────────────┬───────┘
              所有 LLM 能力   │            │ HTTP
              统一走 Python   │            │
              ┌─────────────▼──┐   ┌─────▼──────────┐
              │  ai-wiki-llm   │   │  Chroma / Milvus│  向量库（VECTOR_DRIVER 切换）
              │  (Python FastAPI)│  └────────────────┘
              └───────┬─────────┘
                      │ OpenAI 兼容 API（文档生成 / 多模型调度 / embedding）
                      ▼
           外部大模型 API（OpenAI / Anthropic / DeepSeek / 阿里云 ...）
```

核心设计一句话：**Go 只做业务编排，所有「调模型」的能力统一收口在 Python 侧（ai-wiki-llm）**，Go 侧不存在任何直连模型 API 的代码；多模型调度（低价优先/降级/熔断/限流）也在 Python 侧实现。

## 二、服务组件与职责

| 组件 | 技术栈 | 职责 |
| ---- | ---- | ---- |
| ai-code-wiki | Go 1.22 + Gin + GORM | 业务编排：任务流水线、文档管理、模块依赖图谱、RAG 检索、需求分析、透传调度参数与元信息 |
| ai-wiki-llm | Python 3.11 + FastAPI + LangChain | **模型唯一入口**：文档生成、变更摘要、embedding、向量写入、RAG 重排、多模型调度（低价优先/降级/熔断/限流） |
| MySQL 8.0 | — | 业务库：task_record / code_function_doc / module_relation / doc_modify_log 等 |
| Chroma / Milvus | 向量库 | 文档向量存储与相似度检索（`VECTOR_DRIVER` 切换） |
| Redis 7（可选） | — | ai-wiki-llm 多模型调度的分布式熔断/限流状态（未配置则 fail-open） |
| 外部模型 API | OpenAI 兼容 | DeepSeek / 阿里云百炼 / OpenAI / Anthropic ...（文本生成、embedding） |

## 三、模型交互全景

### 3.1 ai-wiki-llm 对外提供的接口

| 接口 | 用途 | 内部实现 |
| ---- | ---- | ---- |
| `POST /api/generate/doc` | 代码 → 标准化业务文档 | `DocGenerator` → `LLMService`（gpt-4o / claude） |
| `POST /api/generate/diff_log` | 代码 diff → 变更摘要 | 同上（**预留接口，Go 侧当前未调用**） |
| `POST /api/embedding/text` | 文本 → 向量 | `OpenAIEmbeddings`（text-embedding-3-small） |
| `POST /api/vector/upsert_doc` | 写/更新文档向量 | embed + 写 Chroma（供 Go 侧 chroma 驱动复用） |
| `POST /api/rag/rerank` | RAG 候选重排 | 简易算法（关键词重叠 + 得分加权），非模型 |
| `POST /api/chat` | 通用对话（RAG 问答/需求分析） | **多模型调度器 `Scheduler`（低价优先、失败降级、Redis 熔断/限流）** |
| `GET /health` | 健康检查 | — |

### 3.2 系统中所有「模型交互点」

| # | 调用方 | 目标 | 用途 | 模型 | 调度 |
| -- | ---- | ---- | ---- | ---- | ---- |
| 1 | Go `search` / `requirement` → ai-wiki-llm `/api/chat` | Python 调度器 → 各家 API | RAG 问答 / 需求分析 | `model_pool.yaml` 模型池 | ✅ **是** |
| 2 | Go `task_service.generateDoc` → ai-wiki-llm `/api/generate/doc` | Python `LLMService` → 单模型 | 文档生成 | ai-wiki-llm 配置的单模型 | ❌ 否（离线批处理） |
| 3 | 代码变更摘要 | ai-wiki-llm `/api/generate/diff_log` | 预留能力（**Go 侧当前未调用**） | — | ❌ 否 |
| 4 | Go `pkg/vector.EmbedText` → ai-wiki-llm `/api/embedding/text` | Python `OpenAIEmbeddings` | query/文档转向量 | embedding 专用模型 | ❌ 否（**必须固定，禁止调度**，见 4.3） |
| 5 | Go `ChromaClient.UpsertDoc` → ai-wiki-llm `/api/vector/upsert_doc` | Python embed + 写 Chroma | 文档向量写入 | 同上 | ❌ 否 |
| 6 | Go `MilvusClient` → Milvus + `/api/embedding/text` | Go 直连向量库 + Python embed | 向量写入/检索 | 同上 | ❌ 否 |

> 注意：Chroma 的**检索**（`SearchQuery`）是 Go 直连 Chroma HTTP，只有「向量化」这个动作经过 ai-wiki-llm；Milvus 模式下 Go 全程直连向量库（向量化仍走 Python）。**任何模型 API 调用均经过 ai-wiki-llm，Go 侧无直连模型代码。**

### 3.3 为什么模型交互统一收口 Python

1. **单一通路**：模型供应商、密钥、调度策略全部集中在一个地方（ai-wiki-llm），Go 只透传参数，不关心底层是哪个模型、是否降级；
2. **embedding 一致性**：向量化必须固定模型（切换 = 向量空间变化 = 召回失效），集中管理避免误切换；
3. **可观测性**：成本、降级、限流、熔断的埋点日志集中在 Python 侧，与文档生成等其它模型调用同栈；
4. 代价：每次问答比 Go 直连多一跳 HTTP 转发（Go → Python → 模型），但对业务可接受；如需更低延迟可后续将调度下放（见「七、演进」）。

## 四、核心业务流水线

### 4.1 代码解析（入库建文档）

```
CI/GitLab push ──▶ POST /api/v1/webhook/git_push ──▶ 投递 task ──▶ TaskWorker 消费
  └─ git diff（增量，基准分支）─▶ 源码解析（pkg/astgo / astphp）
       └─ generateDoc ─▶ ai-wiki-llm /api/generate/doc ─▶ 标准化业务文档落库(MySQL)
       └─ 模块依赖 AST 识别 ─▶ module_relation
       └─ UpsertDoc ─▶ 向量写入（chroma 经 ai-wiki-llm upsert / milvus 直连）
```

### 4.2 RAG 问答（GET/POST /api/v1/doc/search）

```
用户 query
  1. EmbedText(query) ─▶ ai-wiki-llm /api/embedding/text（embedding，固定模型）
  2. SearchQuery(向量) ─▶ Chroma/Milvus 候选 doc_id（Go 直连向量库）
  3. MySQL 读候选文档 + 读 module_relation（AST ∪ 人工）
  4. 跨模块扩充召回 + 上下文截断组装 prompt
  5. /api/chat ─▶ ai-wiki-llm 多模型调度器（低价优先，失败降级）生成回答
  6. 返回 answer + reference_list + used_model/switch_count/cost（调度元信息透出）
```

### 4.3 需求分析（POST /api/v1/requirement/analyze）

与 4.2 相同的检索流水线（复用 `RetrieveRelatedDocs`），仅第 5 步要求 LLM 输出结构化 JSON；文本生成同样走 `/api/chat` 多模型调度。

### 4.4 文档人工校正 / 重置（向量同步）

```
PUT /api/v1/doc/:doc_id/edit（事务 + 快照日志）
  └─ 异步 UpsertDoc ─▶ 向量库更新（保证检索用最新校正内容）
```

## 五、多模型调度器（ai-wiki-llm `service/scheduler.py`）边界

- **作用域**：`/api/chat`（Go 侧 search / requirement 的文本生成）；
- **能力**：低价优先（平均单价升序）、可重试错误降级切换（429/5xx/超时/网络）、401/403 标记不可用、业务错误不切换、Redis 分布式熔断 + RPM/TPM 限流（Lua 滑动窗口，fail-open）、`model_pool.yaml` 热重载、`force_model` / `force_high_quality` / `estimated_tokens` 参数；
- **明确不做**：embedding（固定模型）、文档生成（离线批处理，走 `LLMService` 单模型）；
- **模型池配置**：`ai-wiki-llm/model_pool.yaml`（密钥 `${ENV}` 占位），与文档生成的 `OPENAI_API_KEY` / `LLM_MODEL` 是两套独立配置。

## 六、配置与环境变量（模型相关）

| 配置 | 归属 | 作用 |
| ---- | ---- | ---- |
| `ai-wiki-llm/model_pool.yaml` | Python 调度器 | 文本生成模型池（name/单价/上下文/rpm/tpm/开关 + global 调度参数） |
| `DEEPSEEK_API_KEY` / `DASHSCOPE_API_KEY` 等 | Python 调度器 | 模型池密钥（`${ENV}` 占位） |
| ai-wiki-llm：`REDIS_ADDR` / `REDIS_HOST`+`REDIS_PORT` / `REDIS_PASSWORD` / `REDIS_DB` | Python 调度器 | 熔断/限流状态存储（可选，未配置 fail-open） |
| `llm.base_url`（`LLM_SERVICE_URL`） | Go → ai-wiki-llm | Go 侧所有 LLM 能力（embedding / 文档生成 / /api/chat）的服务地址 |
| ai-wiki-llm：`OPENAI_API_KEY` / `OPENAI_BASE_URL` / `LLM_MODEL` | ai-wiki-llm | 文档生成 / 变更摘要的单模型 |
| ai-wiki-llm：`EMBEDDING_MODEL` | ai-wiki-llm | embedding 专用模型（默认 text-embedding-3-small） |

## 七、演进建议

1. **文档生成接入模型池**：`generate_doc` 内部改用 `Scheduler`（模型池），文档生成/变更摘要与问答共用一套模型池与熔断限流；
2. **或调度下放到 Go**：如果追求更低延迟，可把调度器整体搬回 Go（Go 直连各家 API），但会牺牲「单一模型通路」与集中可观测性（本方案已明确放弃该路线）；
3. **embedding 切换需谨慎**：换 embedding 模型 = 向量空间变化，需要**全量重建向量库**，不能零切换。