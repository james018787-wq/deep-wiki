#!/usr/bin/env bash
# ============================================================
# ai-code-wiki API 全接口 curl 测试脚本
# 开发环境：http://127.0.0.1:8080
# 鉴权：/api/v1 分组接口需携带 X-Api-Secret（与 API_SECRET_KEY 一致）
# 用法：先修改下方 BASE_URL / API_SECRET，再执行 bash api_test.sh
# ============================================================

set -euo pipefail

# ---------- 基础配置（按需修改） ----------
BASE_URL="http://127.0.0.1:8080"
API_SECRET="your-secret-key"          # 对应环境变量 API_SECRET_KEY；为空则鉴权关闭
AUTH=(-H "Content-Type: application/json" -H "X-Api-Secret: ${API_SECRET}")

# ---------- Mock 数据 ----------
TASK_ID="build-20260815-001"          # 任务唯一标识（重复触发会返回 10005 冲突）
BRANCH="feature/demo-order"           # 代码分支
QUERY="下单支付流程涉及哪些模块和函数"   # 自然语言检索
DOC_ID="1"                            # 文档 ID（可从搜索/修改列表中取真实值替换）
MODULE="order"                        # 业务模块名

echo "=== 1. 健康检查（无需鉴权） ==="
curl -s -w "\n[HTTP %{http_code}]\n" "${BASE_URL}/health"
echo

echo "=== 2. 触发代码解析任务（CI 回调） ==="
curl -s -w "\n[HTTP %{http_code}]\n" -X POST "${BASE_URL}/api/v1/task/trigger" \
  "${AUTH[@]}" \
  -d "{\"task_id\":\"${TASK_ID}\",\"branch\":\"${BRANCH}\"}"
echo

echo "=== 3. 查询任务状态（GET /task/status） ==="
curl -s -w "\n[HTTP %{http_code}]\n" "${AUTH[@]}" \
  "${BASE_URL}/api/v1/task/status?task_id=${TASK_ID}"
echo

echo "=== 4. 任务列表（GET /task/list，分页） ==="
curl -s -w "\n[HTTP %{http_code}]\n" "${AUTH[@]}" \
  "${BASE_URL}/api/v1/task/list?page=1&page_size=20"
echo

echo "=== 5. 自然语言跨模块检索文档（POST /doc/search） ==="
curl -s -w "\n[HTTP %{http_code}]\n" -X POST "${BASE_URL}/api/v1/doc/search" \
  "${AUTH[@]}" \
  -d "{\"query\":\"${QUERY}\"}"
echo

echo "=== 6. 获取所有业务模块（GET /doc/module/list） ==="
curl -s -w "\n[HTTP %{http_code}]\n" "${AUTH[@]}" \
  "${BASE_URL}/api/v1/doc/module/list"
echo

echo "=== 7. 获取文档详情（GET /doc/:doc_id） ==="
curl -s -w "\n[HTTP %{http_code}]\n" "${AUTH[@]}" \
  "${BASE_URL}/api/v1/doc/${DOC_ID}"
echo

echo "=== 8. 人工校正文档（PUT /doc/:doc_id/edit，写操作日志+异步同步向量） ==="
curl -s -w "\n[HTTP %{http_code}]\n" -X PUT "${BASE_URL}/api/v1/doc/${DOC_ID}/edit" \
  "${AUTH[@]}" \
  -d "{
    \"summary\":\"订单下单主流程（人工校正）\",
    \"input_desc\":\"入参：userId、goodsList、addressId\",
    \"output_desc\":\"返回：orderId、payParams\",
    \"process_flow\":\"1.校验库存 2.生成订单 3.扣减库存 4.返回支付参数\",
    \"risk_point\":\"并发超卖风险，需加锁\",
    \"operator\":\"tester\",
    \"remark\":\"curl 测试人工校正\"
  }"
echo

echo "=== 9. 人工校正文档列表（GET /doc/modified/list，content_source=2） ==="
curl -s -w "\n[HTTP %{http_code}]\n" "${AUTH[@]}" \
  "${BASE_URL}/api/v1/doc/modified/list?page=1&page_size=20"
echo

echo "=== 10. 文档迭代变更记录（GET /doc/changelog?doc_id=） ==="
curl -s -w "\n[HTTP %{http_code}]\n" "${AUTH[@]}" \
  "${BASE_URL}/api/v1/doc/changelog?doc_id=${DOC_ID}"
echo

echo "=== 11. 重置文档为原始 AI 版本（POST /doc/:doc_id/reset，恢复 origin_auto_doc） ==="
curl -s -w "\n[HTTP %{http_code}]\n" -X POST "${BASE_URL}/api/v1/doc/${DOC_ID}/reset" \
  "${AUTH[@]}" \
  -d "{\"operator\":\"tester\"}"
echo

echo "=== 12. 查询模块上下游依赖（GET /relation/list?module=&direction=） ==="
# direction=out 下游（该模块依赖谁）/ in 上游（谁依赖该模块）
curl -s -w "\n[HTTP %{http_code}]\n" "${AUTH[@]}" \
  "${BASE_URL}/api/v1/relation/list?module=${MODULE}&direction=out"
echo

echo "=== 13. 手动新增模块依赖（POST /relation/add，写操作日志） ==="
curl -s -w "\n[HTTP %{http_code}]\n" -X POST "${BASE_URL}/api/v1/relation/add" \
  "${AUTH[@]}" \
  -d "{
    \"source_module\":\"order\",
    \"target_module\":\"inventory\",
    \"relation_type\":1,
    \"creator\":\"tester\",
    \"remark\":\"curl 测试新增依赖\"
  }"
echo

echo "=== 14. 删除模块依赖（DELETE /relation，逻辑删除+操作日志） ==="
curl -s -w "\n[HTTP %{http_code}]\n" -X DELETE "${BASE_URL}/api/v1/relation" \
  "${AUTH[@]}" \
  -d "{
    \"source_module\":\"order\",
    \"target_module\":\"inventory\",
    \"relation_type\":1,
    \"operator\":\"tester\",
    \"remark\":\"curl 测试删除依赖\"
  }"
echo

echo "=== 15. 新产品需求分析（POST /requirement/analyze） ==="
curl -s -w "\n[HTTP %{http_code}]\n" -X POST "${BASE_URL}/api/v1/requirement/analyze" \
  "${AUTH[@]}" \
  -d "{\"user_requirement\":\"用户下单时校验库存不足自动提示并支持部分发货\"}"
echo

echo "=== 全部接口测试完成 ==="
