# 多模型调度（优先低价）实现方案

> 状态：已实施完成（M1 → M2 → M3 均完成，测试通过）
> 适用：AI-Code-Wiki RAG 知识库问答系统，LLM 调用层改造

## 一、需求总览

### 业务需求

- 维护多模型配置池，每个模型配置单价、上下文、rpm/tpm、开关；
- 默认调度策略：优先价格最低可用模型；
- 低价模型触发限流 / 超时 / 服务故障 → 自动降级切换下一档；
- 区分可重试错误与业务错误，业务错误不切换模型；
- 熔断器：模型连续失败短时熔断，自动恢复；
- 支持请求级别参数覆盖：`force_model` 强制指定模型、`force_high_quality` 强制不走低价；
- 埋点日志：记录本次请求候选模型列表、实际调用模型、是否降级、token 消耗、成本；
- 原有业务接口兼容不破坏，上层 RAG 业务无需大改。

### 非功能约束

- 不修改原有 RAG 检索链路，只改造 LLM 调用层；
- 分布式部署，限流 / 熔断状态存 Redis，不使用单机内存；
- 最大降级重试次数，防止无限循环；
- 可后台热更新模型配置，不用重启服务。

## 二、总体架构

```
handler ──► service ──► internal/llm.Scheduler ──► client (OpenAI 兼容 /v1/chat/completions)
                             │  ├── 过滤(enable / 上下文 / 高质量阈值)
                             │  ├── 按平均单价升序（低价优先）
                             │  ├── CircuitBreaker(Redis)
                             │  └── RateLimiter(RPM/TPM, Redis 滑动窗口)
                             │  └── pkg/redis 轻量客户端（标准库 RESP 实现）
                             └────► 失败降级: 低价→中价→高价 → 全部失败返回上游错误
```

调度器只作用于**文本生成**；RAG 检索、向量召回、prompt 组装完全不动，embedding 模型保持固定（禁止低价切换，否则向量维度/语义不一致导致召回失效）。

## 三、模块结构（对接现有代码）

```
ai-code-wiki
├─ internal/llm
│  ├─ client.go          #【改造】原 pkg/llm 单模型调用迁移到此处，增强为 OpenAI 兼容 chat/completions
│  │                    #        解析 usage.prompt_tokens / completion_tokens
│  ├─ model_config.go    #【新增】ModelItem/SchedulerOption/SchedulerResult + model.yaml 加载与热重载
│  ├─ model_scheduler.go #【新增】多模型调度核心（低价优先 + 降级链）
│  ├─ circuit_breaker.go #【新增】熔断器（Redis，连续 N 次可重试错误 → 熔断 TTL）
│  └─ rate_limit.go      #【新增】RPM/TPM 分布式限流（Redis 滑动窗口）
├─ internal/service
│  ├─ search_service.go  #【改】askLLM 改走调度器，其余 RAG 检索/组装零改动
│  └─ requirement_service.go #【改】Analyze 的 LLM 调用改走调度器
├─ internal/handler
│  └─ doc.go             #【改】请求体透传 force_model/force_high_quality（可选），响应附加调度元信息
│                        #      绑定经 ShouldBindJSON 自动完成，handler 代码零改动
├─ pkg/redis
│  └─ client.go          #【新增】轻量 Redis 客户端（标准库 RESP2，避免新增 go-redis 依赖）
└─ config
   ├─ model.yaml         #【新增】模型池配置（热重载）
   └─ config.yaml        #【改】新增 redis: 段（熔断/限流状态存储，REDIS_ADDR 等环境变量可覆盖）
```

依赖说明（构建环境无外网，均使用已有依赖或标准库，零新增第三方依赖）：

- ~~`github.com/redis/go-redis/v9`~~ → 自研 `pkg/redis` 轻量客户端（标准库 `net` 实现 RESP2，覆盖 PING/SET/GET/DEL/EXISTS/INCR/EVAL，单连接串行 + 出错重连）；后续如有外网可平滑替换为 go-redis，接口不变；
- ~~`github.com/fsnotify/fsnotify`~~ → 热重载采用周期 mtime 轮询（5s），避免新增依赖。

## 四、model.yaml 配置

```yaml
model_pool:
  - name: qwen-turbo            # 平均价最低，默认低价首选
    provider: aliyun
    api_key: "${QWEN_API_KEY}"  # 密钥不写死进仓库，占位符在加载时从环境变量替换
    base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
    input_price: 0.08
    output_price: 0.16
    max_context: 32000
    rpm: 80
    tpm: 120000
    enable: true
  - name: deepseek-chat
    provider: deepseek
    api_key: "${DEEPSEEK_API_KEY}"
    base_url: "https://api.deepseek.com/v1"
    input_price: 0.14
    output_price: 0.28
    max_context: 64000
    rpm: 60
    tpm: 100000
    enable: true
  - name: gpt-4o-mini
    provider: openai
    api_key: "${OPENAI_API_KEY}"
    base_url: "https://api.openai.com/v1"
    input_price: 0.15
    output_price: 0.60
    max_context: 128000
    rpm: 100
    tpm: 200000
    enable: true

global:
  max_retry_switch: 2          # 最多降级切换 2 次
  circuit_ttl_sec: 30          # 熔断时长（秒）
  circuit_failure_threshold: 3 # 连续 N 次可重试错误触发熔断
  ratelimit_window_sec: 60     # RPM/TPM 滑动窗口（秒）
  high_quality_price_threshold: 0.2  # force_high_quality 时过滤低于该平均单价的模型
```

> Redis 连接配置放在 `config/config.yaml` 的 `redis:` 段（addr/password/db），不放在 model.yaml：
> `REDIS_ADDR`（或 `REDIS_HOST`+`REDIS_PORT`）/ `REDIS_PASSWORD` / `REDIS_DB` 环境变量可覆盖；addr 为空时不启用熔断/限流（fail-open）。

说明：

- 密钥占位符 `"${ENV}"` 加载时从环境变量替换；`model.yaml` 作为模板入库，真实密钥仅经环境变量注入（与既有安全约定一致）。
- `input_price` / `output_price` 单位为 元/1k tokens。
- 修改 model.yaml 后 5s 内自动热重载（mtime 轮询），无需重启；解析失败保留旧配置。

## 五、核心结构体

```go
type ModelItem struct {
    Name        string  `yaml:"name"`
    Provider    string  `yaml:"provider"`
    ApiKey      string  `yaml:"api_key"`
    BaseUrl     string  `yaml:"base_url"`
    InputPrice  float64 `yaml:"input_price"` // 元/1k tokens
    OutputPrice float64 `yaml:"output_price"`
    MaxContext  int     `yaml:"max_context"`
    Rpm         int     `yaml:"rpm"`
    Tpm         int     `yaml:"tpm"`
    Enable      bool    `yaml:"enable"`
}

type SchedulerOption struct {
    ForceModel        string // 非空则直接指定，跳过调度
    ForceHighQuality  bool   // 过滤低于 high_quality_price_threshold 的模型
    MaxSwitchTimes    int    // 未设置时取 global.max_retry_switch
    EstimatedTokenLen int    // 由上层按上下文估算传入
}

type SchedulerResult struct {
    UsedModelName string   // 实际调用模型
    SwitchedCount int      // 降级切换次数
    TokenInput    int      // usage.prompt_tokens
    TokenOutput   int      // usage.completion_tokens
    Cost          float64  // input/1000*input_price + output/1000*output_price
    RetriedModels []string // 尝试失败过的模型列表（含被熔断/限流跳过的）
}
```

## 六、调度主流程

```
输入 messages + SchedulerOption
1. ForceModel 非空 → 直接调用指定模型（不调度、不降级、不设熔断），失败原样返回
2. 过滤 enable=false、max_context < EstimatedTokenLen 的模型
3. ForceHighQuality=true → 过滤 avg_price < high_quality_price_threshold
4. 候选按 (input_price+output_price)/2 升序（低价优先）
5. 循环候选（受 max_retry_switch 限制）：
   a. 熔断检查（Redis key 存在则跳过）
   b. RPM/TPM 限流（Redis 滑动窗口，超限跳过）
   c. 调用 client.Chat
   d. 成功 → 组装 SchedulerResult（含 usage/cost）返回
   e. 失败分类：
      - IsRetriableErr（429、5xx、超时、网络错）→ 连续失败计数+1，继续下一模型
      - 401 鉴权 → 直接写熔断 key（标记该模型不可用），切换下一模型，不重试
      - 非可重试（400 业务拒绝、上下文超限、内容拒绝）→ 直接返回错误，不再切换
6. 候选遍历完 / 切换次数耗尽 → 返回“所有模型服务暂时不可用”（CodeUpstreamError）
```

- `SwitchedCount` 累加每次真实发起过调用的失败切换；被熔断/限流跳过的模型记入 `RetriedModels` 供日志排查。
- 全链失败时沿用原上游错误语义（`CodeUpstreamError`），handler 友好提示，行为与现状一致。

## 七、熔断器（Redis）

- key：`ai_code_wiki:model:circuit:{model_name}`，`SETEX` 过期 = `circuit_ttl_sec`
- 触发：同一模型连续 `circuit_failure_threshold` 次可重试错误；401 直接写 key
- 每次可重试失败 `INCR` 计数（key：`ai_code_wiki:model:circuit:count:{model_name}`，同 TTL 自动清零）；成功则 `DEL` 计数
- 到期自动恢复（TTL 熔断，无半开探测）

## 八、分布式限流（Redis 滑动窗口）

- RPM key：`ai_code_wiki:model:rpm:{model_name}`；TPM key：`ai_code_wiki:model:tpm:{model_name}`
- 滑动窗口实现：`ZREM` 窗口外 + `ZADD(time)` + `ZCOUNT`（或退化 `INCR+EXPIRE` 固定窗口）
- 超限返回 `ErrRateLimited` → 调度器跳过该模型（不计熔断计数）

## 九、handler/service 接入（兼容不破坏）

```json
// POST /api/v1/doc/search  请求（可选字段，缺省走低价优先）
{ "query": "...", "force_model": "", "force_high_quality": false }

// 响应（附加字段，前端可选消费）
{ "code": 0, "data": {
    "answer": "...",
    "reference_list": [...],
    "used_model": "qwen-turbo",
    "switch_count": 1,
    "cost": 0.0023 } }
```

- `SearchReq` / `AnalyzeReq` 增加可选字段；`SearchResult` / `AnalyzeResult` 增加调度元信息（缺省零值，前端不受影响）；
- RAG 检索（`RetrieveRelatedDocs`）、上下文组装、`reference_list` 全部不动，仅 `askLLM` 从 `pkg/llm.Chat` 换成 `scheduler.Chat`；需求分析同理。

## 十、埋点日志（每条问答必打，携带 request_id）

```
used_model, switch_count, force_model, force_high_quality,
estimated_context_token, input_token, output_token, cost,
error_msg, retried_model_list
```

## 十一、风险点 & 兼容方案

| 风险 | 方案 |
|------|------|
| 配置热更新 | 周期 mtime 轮询（5s）检测 `model.yaml`，RWMutex 原子替换模型池；重载失败保留旧配置 |
| 预估 token 不准 | 上下文过滤用预估值；真实超上下文属业务错误，不降级直接返回 |
| 无限切换 | `max_retry_switch` 硬上限；`switched_count` 埋点监控 |
| 分布式限流 | 全部走 Redis 滑动窗口，禁内存计数器 |
| Redis 故障 | 熔断/限流 fail-open（记录告警日志，调度仍可运行），Redis 不作为 LLM 调用新单点；高成本场景可改为 fail-closed |
| 401 鉴权 | 直接写熔断 key 标记不可用并切换下一模型，不重试 |
| 密钥入库 | model.yaml 用 `${ENV}` 占位，真实密钥仅环境变量注入 |
| embedding 一致性 | 调度器只作用于文本生成；RAG embedding 保持原模型，禁止切换 |
| 构建环境无外网 | 自研 `pkg/redis` 轻量客户端（标准库 RESP2）替代 go-redis；后续可平滑替换 |

## 十二、实施步骤

- **M1 基础**：`internal/llm/client.go`（OpenAI 兼容 + usage 解析）、`model_config.go`（加载 + 热重载 + 占位符）、`model_scheduler.go`（低价优先 + 降级链 + 错误分类）；`search` / `requirement` 接入；handler 可选字段；埋点日志；model.yaml 模板 【完成】
- **M2 分布式**：`circuit_breaker.go`、`rate_limit.go`（Redis，Lua 滑动窗口 + 原子熔断），Redis 故障 fail-open 【完成】
- **M3 收尾**：`go build/vet/test` 全绿、README/env 清单、安全自检后提交 【完成】