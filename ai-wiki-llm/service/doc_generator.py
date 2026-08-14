"""文档生成：Prompt 封装 + 结构化输出解析。"""

import json
import re
from typing import Any

from schema.models import GenerateDiffLogResponse, GenerateDocResponse
from service.llm_service import LLMService
from utils.errors import LLMError
from utils.logging import logger

# 文档生成系统提示词：约束输出为标准化 JSON。
DOC_GENERATE_SYSTEM_PROMPT = """你是一名资深架构师，负责把函数源码转换为标准化业务文档。
要求：
1. 只输出一个 JSON 对象，不要包含任何额外解释或 Markdown 代码块标记。
2. 字段严格为：func_name, summary, input_desc, output_desc, process_flow, rely_modules, risk_point。
3. summary 为一句话业务摘要（不超过50字）。
4. input_desc/output_desc 使用简洁中文，描述入参含义与返回值。
5. process_flow 按步骤列出业务执行流程，用中文分号分隔。
6. rely_modules 为依赖模块 json 数组（跨模块调用），无则输出 []。
7. risk_point 描述业务风险点，无风险输出空字符串。
8. 分析代码必须以【单个函数】为单位，不要展开整个文件。"""

# diff 摘要生成系统提示词。
DIFF_LOG_SYSTEM_PROMPT = """你是一名发布评审专家，请对比新旧代码，输出代码变更摘要。
要求：
1. 只输出一个 JSON 对象：change_summary, business_impact, attention。
2. 中文表述，简洁明确。
3. change_summary 描述代码逻辑变化；business_impact 说明影响到的业务范围；
   attention 列出上线注意事项，无则空字符串。"""


class DocGenerator:
    """文档生成服务：根据代码生成标准化业务文档 JSON。"""

    def __init__(self, llm: LLMService) -> None:
        self._llm = llm

    def generate_doc(self, module_name: str, file_path: str, code_content: str) -> dict[str, Any]:
        """根据代码生成标准化业务文档。

        Args:
            module_name: 所属业务模块
            file_path: 源码文件路径
            code_content: 函数源码片段（最小切片单元=单个函数）

        Returns:
            标准化文档 dict（见 schema.GenerateDocResponse）
        """
        user_content = (
            f"所属模块: {module_name}\n"
            f"文件路径: {file_path}\n"
            f"函数源码:\n```\n{code_content}\n```"
        )
        raw = self._llm.chat(DOC_GENERATE_SYSTEM_PROMPT, user_content)
        data = self._parse_json(raw)

        # 兜底默认值，保证字段完整性
        result = GenerateDocResponse(
            module_name=module_name,
            file_path=file_path,
            func_name=str(data.get("func_name", "")),
            summary=str(data.get("summary", "")),
            input_desc=str(data.get("input_desc", "")),
            output_desc=str(data.get("output_desc", "")),
            process_flow=str(data.get("process_flow", "")),
            rely_modules=self._dump_rely_modules(data.get("rely_modules", [])),
            risk_point=str(data.get("risk_point", "")),
        )
        return result.model_dump()

    def generate_diff_log(self, old_code: str, new_code: str) -> dict[str, str]:
        """代码 diff 生成变更摘要。

        Returns:
            变更摘要 dict（见 schema.GenerateDiffLogResponse）
        """
        user_content = f"变更前代码:\n```\n{old_code}\n```\n\n变更后代码:\n```\n{new_code}\n```"
        raw = self._llm.chat(DIFF_LOG_SYSTEM_PROMPT, user_content)
        data = self._parse_json(raw)

        result = GenerateDiffLogResponse(
            change_summary=str(data.get("change_summary", "")),
            business_impact=str(data.get("business_impact", "")),
            attention=str(data.get("attention", "")),
        )
        return result.model_dump()

    @staticmethod
    def _parse_json(raw: str) -> dict[str, Any]:
        """解析模型输出 JSON，容忍代码块包裹等噪音。"""
        text = raw.strip()
        # 去掉 ```json ... ``` 包裹
        code_block = re.search(r"```(?:json)?\s*(.*?)\s*```", text, re.DOTALL)
        if code_block:
            text = code_block.group(1).strip()
        try:
            data = json.loads(text)
        except json.JSONDecodeError as e:
            logger.error("模型输出JSON解析失败: %s | 原文: %s", e, raw)
            raise LLMError(message="模型输出解析失败，请重试", cause=e) from e
        if not isinstance(data, dict):
            raise LLMError(message="模型输出格式异常，期望 JSON 对象")
        return data

    @staticmethod
    def _dump_rely_modules(value: Any) -> str:
        """统一输出依赖模块 json 字符串。"""
        if isinstance(value, str):
            try:
                parsed = json.loads(value)
            except json.JSONDecodeError:
                parsed = [value]
        elif isinstance(value, list):
            parsed = value
        else:
            parsed = []
        return json.dumps(parsed, ensure_ascii=False)