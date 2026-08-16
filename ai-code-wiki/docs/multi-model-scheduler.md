# 多模型调度（优先低价）实现方案

> 状态：已实施完成，模型调度统一收口在 Python（ai-wiki-llm），Go 侧仅编排与透传
> 适用：AI-Code-Wiki RAG 知识库问答系统
> 设计原则：**所有「调模型」能力集中在 ai-wiki-llm，Go 不直连任何模型 API**

## 一、需求总览

### 业务需求

- 维护多模型配置池，每个模型配置单价、上下文、rpm/tpm、开关；
- 默认调度策略：优先价格最低可用模型；
- 低价模型触发限流 / 超时 / 服务故障 → 自动降级切换下一档；
- 区分可重试错误与业务错误，业务错误不切换模型；
- 熔断器：模型连续失败短时熔断，自动恢复；
- 支持请求级别参数覆盖：`force_model` 强制指定模型、`force_high_quality` 强制不走低价；
- 埋点日志：记录候选模型、实际调用模型、是否降级、token 消耗、成本；
- 原有业务接口兼容不破坏，上层 RAG 业务无需大改。

### 非功能约束

- 不修改原有 RAG 检索链路，只改造 LLM 调用层；
- 分布式部署，限流 / 熔断状态存 Redis，不使用单机内存；
- 最大降级重试次数，防止无限循环；
- 可后台热更新模型配置，不用重启服务。

## 二、总体架构

```
Go(search/requirement) ── POST /api/chat（透传 force_model / force_high_quality / estimated_tokens）
       │
       ▼
ai-wiki-llm service/scheduler.py
  ├── model_pool.py      # model_pool.yaml 加载 + ${ENV} 密钥占位 + 热重载
  ├── Scheduler          # 低价优先 + 降级链 + 错误分类
  ├── CircuitBreaker     # Redis 分布式熔断（fail-open）
  └── RateLimiter        # RPM/TPM Redis 滑动窗口（fail-open）
       │
       ▼  OpenAI 兼容 API（openai 客户端，base_url 指向各家）
DeepSeek / 阿里云百炼 / OpenAI ...
```

调度只作用于**文本生成**（`/api/chat`）；RAG 检索、向量召回、prompt 组装在 Go 侧不变，embedding 固定模型（禁止调度，否则向量维度/语义不一致导致召回失效）。

## 三、模块结构

```
ai-wiki-llm
├─ model_pool.yaml       #【新增】模型池配置（热重载，密钥 ${ENV} 占位）
├─ service/model_pool.py #【新增】ModelItem/GlobalConfig + yaml 加载与 mtime 热重载
├─ service/scheduler.py  #【新增】Scheduler（低价优先+降级链） + CircuitBreaker + RateLimiter（Redis Lua）
├─ schema/models.py      #【改】ChatRequest 增加 force_model/force_high_quality/estimated_tokens；新增 ChatResponse
├─ main.py               #【改】/api/chat 改走 Scheduler，返回回答 + 调度元信息
└─ requirements.txt      #【改】新增 redis==5.0.7

ai-code-wiki
├─ internal/service/llm_chat.go      #【新增】调用 ai-wiki-llm /api/chat，透传调度参数、透出元信息
├─ internal/service/search_service.go      #【改】askLLM 走 /api/chat
└─ internal/service/requirement_service.go #【改】Analyze 走 /api/chat
```

> 曾经短暂实现于 Go 侧（internal/llm + pkg/redis + config/model.yaml）的方案已整体移除，改为收口 Python，避免两套实现并存。

## 四、model_pool.yaml 配置

```yaml
model_pool:
  - name: qwen-turbo            # 平均价最低，默认低价首选
    provider: aliyun
    api_key: "${QWEN_API_KEY}"  # 密钥不写死进仓库，加载时从环境变量替换
    base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
    input_price: 0.0003
    output_price: 0.0006
    max_context: 100000
    rpm: 60
    tpm: 100000
    enable: true

global:
  max_retry_switch: 2          # 最多降级切换 2 次
  circuit_ttl_sec: 30          # 熔断时长（秒）
  circuit_failure_threshold: 3 # 连续 N 次可重试错误触发熔断
  ratelimit_window_sec: 60     # RPM/TPM 滑动窗口（秒）
  high_quality_price_threshold: 0.2  # force_high_quality 时过滤低于该平均单价的模型
```

说明：

- 密钥占位符 `"${ENV}"` 加载时从环境变量替换（`MODEL_POOL_FILE` 可覆盖配置文件路径，默认 `model_pool.yaml`）；
- `input_price` / `output_price` 单位为 元/1k tokens；
- 修改 model_pool.yaml 后约 5s 自动热重载（mtime 轮询），无需重启；解析失败保留旧配置。

## 五、核心模型（Python）

```python
class ModelItem:        # name / provider / api_key / base_url / input_price / output_price / max_context / rpm / tpm / enable
    @property avg_price # (input_price + output_price) / 2

class Scheduler.chat(system, user, force_model="", force_high_quality=False, estimated_tokens=0)
    # 返回 {answer, used_model, switch_count, input_token, output_token, cost, retried_models}
```

## 六、调度主流程

```
1. force_model 非空 → 直连指定模型（不调度、不降级、不设熔断），失败原样返回
2. 过滤 enable=false、max_context < estimated_tokens 的模型
3. force_high_quality=true → 过滤 avg_price < high_quality_price_threshold
4. 候选按 (input_price+output_price)/2 升序（低价优先）
5. 循环候选（受 max_retry_switch 限制）：
   a. 熔断检查（Redis key 存在则跳过）
   b. RPM/TPM 限流（Redis 滑动窗口，超限跳过）
   c. 调用 client.chat.completions.create（openai 客户端）
   d. 成功 → 组装结果（含 usage/cost）返回
   e. 失败分类：
      - 可重试（429、5xx、超时、网络）→ 连续失败计数+1，切换下一模型
      - 401/403 鉴权 → 直接写熔断 key（标记不可用），切换下一模型
      - 非可重试（400/404、上下文超限、内容拒绝）→ 直接返回错误，不再切换
6. 候选遍历完 / 切换次数耗尽 → LLMError（“所有模型服务暂时不可用”）
```

- `switch_count` 累加每次真实发起过调用的失败切换；被熔断/限流跳过的模型记入 `retried_models` 供日志排查；
- Go 侧把 Python 返回的调度元信息透出到 `/api/v1/doc/search` 与 `/api/v1/requirement/analyze` 响应。

## 七、熔断器（Redis）

- key：`ai_code_wiki:model:circuit:{model_name}`，SET EX = `circuit_ttl_sec`
- 触发：同一模型连续 `circuit_failure_threshold` 次可重试错误（Lua 原子 INCR+SET）；401/403 直接写 key
- 成功调用 DEL 计数；到期自动恢复（TTL 熔断，无半开探测）
- Redis 未配置/故障 → fail-open（记录告警日志，调度仍可运行）
- 与 Go 侧多轮对话会话记忆（`chat:meta:*` / `chat:msgs:*`）共用同一 Redis，key 命名空间不同互不影响

## 八、分布式限流（Redis 滑动窗口）

- RPM key：`ai_code_wiki:model:rpm:{model_name}`；TPM key：`ai_code_wiki:model:tpm:{model_name}`
- Lua 原子：ZREM 窗口外 + ZADD + 求和校验 + EXPIRE
- 超限 → 调度器跳过该模型（不计熔断计数）

## 九、API 接入（兼容不破坏）

```json
// POST /api/v1/doc/search  请求（可选字段，缺省走低价优先）
{ "query": "...", "force_model": "", "force_high_quality": false }

// 响应（附加字段，前端可选消费）
{ "code": 0, "data": {
    "answer": "...",
    "reference_list": [...],
    "used_model": "qwen-turbo",
    "switch_count": 1,
    "cost": 0.0009 } }
```

- Go `SearchReq` / `AnalyzeReq` 可选字段透传给 ai-wiki-llm `/api/chat`；`SearchResult` / `AnalyzeResult` 附加调度元信息（缺省零值，前端不受影响）；
- RAG 检索（`RetrieveRelatedDocs`）、上下文组装、`reference_list` 全部不动。

## 十、埋点日志

- Python 侧：`[scheduler]` 记录候选/实际模型、失败切换、Redis 熔断/限流告警；
- Go 侧：`[search]` / `[requirement]` 记录 `used_model, switch_count, force_model, force_high_quality, estimated_context_token, input_token, output_token, cost, retried_model_list`（携带 request_id）。

## 十一、风险点 & 兼容方案

| 风险 | 方案 |
|------|------|
| 配置热更新 | mtime 轮询（5s）热重载 model_pool.yaml，RWMutex 原子替换；失败保留旧配置 |
| 预估 token 不准 | 上下文过滤用预估值；真实超上下文属业务错误，不降级直接返回 |
| 无限切换 | `max_retry_switch` 硬上限；`switch_count` 埋点监控 |
| 分布式限流 | 全部走 Redis 滑动窗口，禁内存计数器 |
| Redis 故障 | 熔断/限流 fail-open（记录告警日志，调度仍可运行），Redis 不作为新单点 |
| 401 鉴权 | 直接写熔断 key 标记不可用并切换下一模型，不重试 |
| 密钥入库 | model_pool.yaml 用 `${ENV}` 占位，真实密钥仅环境变量注入 |
| embedding 一致性 | 调度只作用于文本生成；RAG embedding 保持原模型，禁止调度 |
| 多一跳 HTTP | Go→Python→模型 相比直连多一次转发，可接受；追求低延迟可后续调度下放 Go |

## 十二、实施步骤（已完成）

1. Python 侧：`model_pool.yaml` + `service/model_pool.py`（加载/热重载/占位符）+ `service/scheduler.py`（低价优先 + 降级链 + 错误分类 + Redis 熔断/限流）+ `/api/chat` 透传参数与返回元信息 + `requirements.txt` 加 redis-py；
2. Go 侧：`internal/service/llm_chat.go`（调用 /api/chat）+ search/requirement 改走调度接口并透出元信息；移除此前 Go 侧 `internal/llm`、`pkg/redis`、`config/model.yaml`；
3. 校验：`go build/vet/test` 全绿；Python 语法检查通过；文档与 compose 同步更新。