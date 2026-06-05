"""
Synthesizer agent — uses Claude to combine summarized research findings into
a coherent answer with inline citations.

Wire contract:
  POST /invoke
  Body: {
    "question": "user's original question",
    "findings": [
      {"url": "...", "title": "...", "bullets": ["...", "..."]},
      ...
    ]
  }
  Returns: {"answer": "markdown answer with [1][2] inline citations"}
"""
from __future__ import annotations

import logging
import os
from typing import List

from anthropic import Anthropic
from dotenv import load_dotenv
from fastapi import FastAPI, Header, HTTPException
from pydantic import BaseModel, Field

load_dotenv()

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(name)s %(levelname)s %(message)s",
)
log = logging.getLogger("synthesizer")

ANTHROPIC_API_KEY = os.getenv("ANTHROPIC_API_KEY")
if not ANTHROPIC_API_KEY:
    log.warning("ANTHROPIC_API_KEY is not set; /invoke will return HTTP 500")
client = Anthropic(api_key=ANTHROPIC_API_KEY) if ANTHROPIC_API_KEY else None

MODEL = "claude-sonnet-4-6"
app = FastAPI(title="AgentMesh Synthesizer")


class Finding(BaseModel):
    url: str = ""
    title: str = ""
    bullets: List[str] = Field(default_factory=list)


class SynthesizeRequest(BaseModel):
    question: str
    findings: List[Finding]


class SynthesizeResponse(BaseModel):
    answer: str  # markdown


def _format_findings(findings: List[Finding]) -> str:
    parts: List[str] = []
    for i, f in enumerate(findings, start=1):
        bullets = "\n".join(f"  - {b}" for b in f.bullets) or "  - (no points extracted)"
        parts.append(f"[{i}] {f.title or f.url}\n{bullets}\nSource: {f.url}")
    return "\n\n".join(parts) if parts else "(no findings)"


PROMPT = """You are a research synthesis assistant. Given a question and a list of \
research findings, produce a clear, well-structured answer in Markdown.

Requirements:
- Cite sources inline as [1], [2], ... matching the finding numbers below.
- Use short paragraphs and headings where they help readability.
- End with a "## Sources" section that lists each cited finding number, title, and URL.
- Stick to what the findings support. Do not invent facts.

Question: {question}

Findings:
{findings}

Answer (Markdown):"""


@app.post("/invoke", response_model=SynthesizeResponse)
def invoke(
    req: SynthesizeRequest,
    x_agentmesh_capability: str = Header(default="synthesize"),
) -> SynthesizeResponse:
    if client is None:
        raise HTTPException(status_code=500, detail="ANTHROPIC_API_KEY not configured")

    log.info(
        "synthesize question=%r findings=%d capability=%s",
        req.question[:60], len(req.findings), x_agentmesh_capability,
    )

    try:
        msg = client.messages.create(
            model=MODEL,
            max_tokens=2000,
            messages=[{
                "role": "user",
                "content": PROMPT.format(
                    question=req.question,
                    findings=_format_findings(req.findings),
                ),
            }],
        )
    except Exception as exc:
        log.exception("anthropic call failed")
        raise HTTPException(status_code=502, detail=f"anthropic error: {exc}")

    answer = msg.content[0].text if msg.content else ""
    log.info("returning %d chars", len(answer))
    return SynthesizeResponse(answer=answer)


@app.get("/health")
def health() -> dict:
    return {"status": "ok", "agent": "synthesizer", "anthropic_configured": client is not None}


if __name__ == "__main__":
    import uvicorn

    port = int(os.getenv("PORT", "8006"))
    uvicorn.run(app, host="0.0.0.0", port=port)
