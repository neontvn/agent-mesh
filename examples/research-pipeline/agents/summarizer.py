"""
Summarizer agent — uses Claude to turn a passage of text into concise bullets.

Wire contract:
  POST /invoke
  Body: {"text": "...", "title": "optional context", "max_bullets": 5}
  Returns: {"bullets": ["...", "..."]}
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
log = logging.getLogger("summarizer")

ANTHROPIC_API_KEY = os.getenv("ANTHROPIC_API_KEY")
if not ANTHROPIC_API_KEY:
    log.warning("ANTHROPIC_API_KEY is not set; /invoke will return HTTP 500")
client = Anthropic(api_key=ANTHROPIC_API_KEY) if ANTHROPIC_API_KEY else None

MODEL = "claude-sonnet-4-6"
app = FastAPI(title="AgentMesh Summarizer")


class SummarizeRequest(BaseModel):
    text: str = Field(..., min_length=1)
    title: str = ""
    max_bullets: int = Field(default=5, ge=1, le=10)


class SummarizeResponse(BaseModel):
    bullets: List[str]


PROMPT = """You are a research summarization assistant. Given a passage of text, \
extract the most important factual points as concise bullet points. Each bullet \
should be a complete sentence that stands on its own. Do not editorialize or speculate.

Title (for context): {title}

Text:
{text}

Return up to {max_bullets} bullet points, one per line, each starting with "- ". \
No preamble, no commentary, no trailing notes."""


@app.post("/invoke", response_model=SummarizeResponse)
def invoke(
    req: SummarizeRequest,
    x_agentmesh_capability: str = Header(default="summarize"),
) -> SummarizeResponse:
    if client is None:
        raise HTTPException(status_code=500, detail="ANTHROPIC_API_KEY not configured")

    log.info(
        "summarize %d chars title=%r capability=%s",
        len(req.text), req.title[:40], x_agentmesh_capability,
    )

    try:
        msg = client.messages.create(
            model=MODEL,
            max_tokens=600,
            messages=[{
                "role": "user",
                "content": PROMPT.format(
                    title=req.title or "(none)",
                    text=req.text,
                    max_bullets=req.max_bullets,
                ),
            }],
        )
    except Exception as exc:
        log.exception("anthropic call failed")
        raise HTTPException(status_code=502, detail=f"anthropic error: {exc}")

    raw = msg.content[0].text if msg.content else ""
    bullets = [line[2:].strip() for line in raw.splitlines() if line.startswith("- ")]

    log.info("returning %d bullets", len(bullets))
    return SummarizeResponse(bullets=bullets)


@app.get("/health")
def health() -> dict:
    return {"status": "ok", "agent": "summarizer", "anthropic_configured": client is not None}


if __name__ == "__main__":
    import uvicorn

    port = int(os.getenv("PORT", "8004"))
    uvicorn.run(app, host="0.0.0.0", port=port)
