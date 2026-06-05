"""
Planner agent — orchestrator for the AgentMesh research pipeline.

The planner serves a small web UI at / and a POST /research endpoint that
streams NDJSON events as it works through the pipeline:

  1. Decompose the user question into 3-4 sub-questions (Claude).
  2. For each sub-question: invoke `search` through the mesh.
  3. For each search-result URL: invoke `scrape`.
  4. For each scraped passage: invoke `summarize`.
  5. Send all summarized findings to `synthesize` for the final answer.

The planner itself is a mesh CLIENT — it speaks gRPC directly to the
control plane and to peer sidecars via the Python wrapper in
../lib/agentmesh_client.py. It does not run a sidecar.
"""
from __future__ import annotations

import json
import logging
import os
import pathlib
import sys
from contextlib import asynccontextmanager
from typing import Any

from anthropic import Anthropic
from dotenv import load_dotenv
from fastapi import FastAPI, HTTPException
from fastapi.responses import StreamingResponse
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel, Field

# Make the lib/ sibling directory importable so agentmesh_client + generated
# protos resolve.
_HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(_HERE.parent / "lib"))

from agentmesh_client import MeshClient, MeshError  # noqa: E402

load_dotenv()

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(name)s %(levelname)s %(message)s",
)
log = logging.getLogger("planner")

ANTHROPIC_API_KEY = os.getenv("ANTHROPIC_API_KEY")
if not ANTHROPIC_API_KEY:
    log.warning("ANTHROPIC_API_KEY is not set; /research will return HTTP 500")
claude = Anthropic(api_key=ANTHROPIC_API_KEY) if ANTHROPIC_API_KEY else None

CONTROL_PLANE_ADDR = os.getenv("AGENTMESH_CONTROL_PLANE", "localhost:9091")
MODEL = "claude-sonnet-4-6"

# The mesh client is initialized on app startup; kept in module scope so
# the request handler can use it directly.
mesh: MeshClient | None = None


@asynccontextmanager
async def lifespan(_app: FastAPI):
    global mesh
    log.info("connecting to control plane at %s", CONTROL_PLANE_ADDR)
    mesh = MeshClient(CONTROL_PLANE_ADDR)
    yield
    if mesh is not None:
        mesh.close()


app = FastAPI(title="AgentMesh Research Planner", lifespan=lifespan)


class ResearchRequest(BaseModel):
    question: str = Field(..., min_length=3)


# ---------- decomposition ----------

DECOMPOSE_PROMPT = """You are a research planning assistant. Given a user's research \
question, break it into 3-4 specific sub-questions that, together, will let you \
research the answer comprehensively. Each sub-question should be searchable on its \
own as a web query.

Question: {question}

Return ONLY the sub-questions, one per line, no numbering, no preamble, no commentary."""


def decompose(question: str) -> list[str]:
    if claude is None:
        raise RuntimeError("ANTHROPIC_API_KEY not configured")
    msg = claude.messages.create(
        model=MODEL,
        max_tokens=400,
        messages=[{"role": "user", "content": DECOMPOSE_PROMPT.format(question=question)}],
    )
    text = msg.content[0].text if msg.content else ""
    subs = [line.strip() for line in text.splitlines() if line.strip()]
    return subs[:4]


# ---------- NDJSON streaming helper ----------

def event(name: str, **data: Any) -> bytes:
    """Encode an event as a single NDJSON line."""
    return (json.dumps({"event": name, **data}) + "\n").encode("utf-8")


# ---------- the orchestrator ----------

@app.post("/research")
def research(req: ResearchRequest):
    if mesh is None:
        raise HTTPException(status_code=500, detail="mesh client not initialized")
    if claude is None:
        raise HTTPException(status_code=500, detail="ANTHROPIC_API_KEY not configured")

    question = req.question
    log.info("research question=%r", question)

    def gen():
        yield event("trace", message=f"Starting research: {question!r}")

        # 1. Decompose
        yield event("trace", message="Decomposing into sub-questions...")
        try:
            sub_questions = decompose(question)
        except Exception as exc:
            yield event("error", message=f"Decompose failed: {exc}")
            return
        yield event("trace", message=f"Decomposed into {len(sub_questions)} sub-questions:")
        for sq in sub_questions:
            yield event("trace", message=f"  - {sq}")

        # 2. Search each sub-question (fan out via mesh)
        urls: list[dict[str, str]] = []
        for sq in sub_questions:
            yield event("trace", message=f"Searching: {sq!r}")
            try:
                result = mesh.invoke("search", {"query": sq, "max_results": 3}, caller_id="planner")
            except MeshError as exc:
                yield event("trace", message=f"  search failed: {exc}")
                continue
            for r in result.get("results", []):
                urls.append({"url": r.get("url", ""), "title": r.get("title", "")})
        yield event("trace", message=f"Collected {len(urls)} URLs")

        # 3. Scrape each URL
        scraped: list[dict[str, str]] = []
        for u in urls:
            if not u["url"]:
                continue
            yield event("trace", message=f"Scraping: {u['url']}")
            try:
                result = mesh.invoke(
                    "scrape",
                    {"url": u["url"], "max_chars": 5000},
                    caller_id="planner",
                )
            except MeshError as exc:
                yield event("trace", message=f"  scrape failed: {exc}")
                continue
            scraped.append({
                "url": u["url"],
                "title": result.get("title") or u["title"],
                "text": result.get("text", ""),
            })
        yield event("trace", message=f"Scraped {len(scraped)} pages")

        # 4. Summarize each
        findings: list[dict[str, Any]] = []
        for s in scraped:
            if not s["text"]:
                continue
            yield event("trace", message=f"Summarizing: {s['title']!r}")
            try:
                result = mesh.invoke(
                    "summarize",
                    {"text": s["text"], "title": s["title"], "max_bullets": 4},
                    caller_id="planner",
                )
            except MeshError as exc:
                yield event("trace", message=f"  summarize failed: {exc}")
                continue
            findings.append({
                "url": s["url"],
                "title": s["title"],
                "bullets": result.get("bullets", []),
            })
        yield event("trace", message=f"Summarized {len(findings)} findings")

        # 5. Synthesize
        yield event("trace", message="Synthesizing final answer...")
        try:
            result = mesh.invoke(
                "synthesize",
                {"question": question, "findings": findings},
                caller_id="planner",
            )
        except MeshError as exc:
            yield event("error", message=f"Synthesize failed: {exc}")
            return

        yield event("answer", answer=result.get("answer", ""), findings=findings)
        yield event("trace", message="Done.")

    return StreamingResponse(gen(), media_type="application/x-ndjson")


@app.get("/health")
def health() -> dict:
    return {
        "status": "ok",
        "agent": "planner",
        "anthropic_configured": claude is not None,
        "mesh_addr": CONTROL_PLANE_ADDR,
    }


# Mount the static web UI last so it doesn't shadow the API routes.
WEB_DIR = _HERE.parent / "web"
if WEB_DIR.exists():
    app.mount("/", StaticFiles(directory=str(WEB_DIR), html=True), name="web")


if __name__ == "__main__":
    import uvicorn

    port = int(os.getenv("PORT", "8000"))
    uvicorn.run(app, host="0.0.0.0", port=port)
