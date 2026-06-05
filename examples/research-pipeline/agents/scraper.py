"""
Scraper agent — fetches a URL and returns cleaned, truncated text.

Wire contract (called via the sidecar's HTTP forward):
  POST /invoke
  Body: {"url": "...", "max_chars": 8000}
  Returns: {"url": "...", "title": "...", "text": "..."}
"""
from __future__ import annotations

import logging
import os

import requests
from bs4 import BeautifulSoup
from dotenv import load_dotenv
from fastapi import FastAPI, Header, HTTPException
from pydantic import BaseModel, Field

load_dotenv()

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(name)s %(levelname)s %(message)s",
)
log = logging.getLogger("scraper")

app = FastAPI(title="AgentMesh Scraper")


class ScrapeRequest(BaseModel):
    url: str
    max_chars: int = Field(default=8000, ge=500, le=50000)


class ScrapeResponse(BaseModel):
    url: str
    title: str = ""
    text: str = ""


@app.post("/invoke", response_model=ScrapeResponse)
def invoke(
    req: ScrapeRequest,
    x_agentmesh_capability: str = Header(default="scrape"),
) -> ScrapeResponse:
    log.info(
        "scrape url=%s max_chars=%d capability=%s",
        req.url, req.max_chars, x_agentmesh_capability,
    )

    try:
        resp = requests.get(
            req.url,
            timeout=15,
            headers={"User-Agent": "Mozilla/5.0 (AgentMesh/Scraper)"},
        )
        resp.raise_for_status()
    except Exception as exc:
        log.exception("fetch failed")
        raise HTTPException(status_code=502, detail=f"fetch {req.url}: {exc}")

    soup = BeautifulSoup(resp.text, "html.parser")

    # Strip noise. Keep main article-y content.
    for tag in soup(["script", "style", "nav", "footer", "aside", "noscript", "form"]):
        tag.decompose()

    title = (soup.title.string or "").strip() if soup.title and soup.title.string else ""
    text = " ".join(soup.get_text(separator=" ").split())

    if len(text) > req.max_chars:
        text = text[: req.max_chars].rstrip() + "..."

    log.info("returning title=%r %d chars", title[:60], len(text))
    return ScrapeResponse(url=req.url, title=title, text=text)


@app.get("/health")
def health() -> dict:
    return {"status": "ok", "agent": "scraper"}


if __name__ == "__main__":
    import uvicorn

    port = int(os.getenv("PORT", "8003"))
    uvicorn.run(app, host="0.0.0.0", port=port)
