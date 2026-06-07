"""Persona Builder — capability "persona".

Input:  {"role": "<job title>"}
Output: {"role": ..., "persona": "<candidate persona>"}
"""

from _serve import run_agent


def handle(_capability: str, payload: dict) -> dict:
    role = payload.get("role", "Software Engineer")
    persona = (
        f"A mid-level candidate for {role}: ~5 years of experience, strong on "
        f"fundamentals, has shipped production systems, and is motivated by "
        f"ownership and clear impact."
    )
    return {"role": role, "persona": persona}


if __name__ == "__main__":
    run_agent(handle, 8001)
