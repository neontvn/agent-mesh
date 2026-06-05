"""
Searcher agent — wraps Tavily web search behind the AgentMesh sidecar contract.

The sidecar forwards inbound A2A Invoke calls here as HTTP POST /invoke with:
  - Body: a JSON payload like {"query": "...", "max_results": 5}
  - Headers:
      Content-Type: application/json
      X-AgentMesh-Capability: search
      X-AgentMesh-Meta-<k>: <v>   (optional, ignored here)

Response body becomes the gRPC Invoke response payload, so we return JSON
like {"results": [...]} that downstream agents can consume.
"""
from __future__ import annotations

import logging
import os
from typing import List

from dotenv import load_dotenv
from fastapi import FastAPI, Header, HTTPException
from pydantic import BaseModel, Field
from tavily import TavilyClient

# Load .env if present (no error if absent).
load_dotenv()

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(name)s %(levelname)s %(message)s",
)
log = logging.getLogger("searcher")

TAVILY_API_KEY = os.getenv("TAVILY_API_KEY")
if not TAVILY_API_KEY:
    log.warning("TAVILY_API_KEY is not set; /invoke will return HTTP 500")
tavily = TavilyClient(api_key=TAVILY_API_KEY) if TAVILY_API_KEY else None

app = FastAPI(title="AgentMesh Searcher")


# ---------- wire types ----------

class SearchRequest(BaseModel):
    query: str = Field(..., description="Natural-language search query")
    max_results: int = Field(default=5, ge=1, le=10)


class SearchResult(BaseModel):
    title: str = ""
    url: str = ""
    content: str = ""


class SearchResponse(BaseModel):
    results: List[SearchResult]


# ---------- handlers ----------

@app.post("/invoke", response_model=SearchResponse)
def invoke(
    req: SearchRequest,
    x_agentmesh_capability: str = Header(default="search"),
) -> SearchResponse:
    if tavily is None:
        raise HTTPException(status_code=500, detail="TAVILY_API_KEY not configured")

    log.info(
        "search query=%r max_results=%d capability=%s",
        req.query, req.max_results, x_agentmesh_capability,
    )

    try:
        result = tavily.search(
            query=req.query,
            max_results=req.max_results,
            search_depth="basic",
        )
    except Exception as exc:
        log.exception("tavily call failed")
        raise HTTPException(status_code=502, detail=f"tavily error: {exc}")

    items = [
        SearchResult(
            title=r.get("title", ""),
            url=r.get("url", ""),
            content=r.get("content", ""),
        )
        for r in result.get("results", [])
    ]
    log.info("returning %d results", len(items))
    return SearchResponse(results=items)


@app.get("/health")
def health() -> dict:
    return {"status": "ok", "agent": "searcher", "tavily_configured": tavily is not None}


# ---------- entrypoint ----------

if __name__ == "__main__":
    import uvicorn

    port = int(os.getenv("PORT", "8001"))
    uvicorn.run(app, host="0.0.0.0", port=port)
