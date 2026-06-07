"""Critic — capability "critique".

Input:  {"questions": ["...", ...]}
Output: {"critique": ["note about Q1", ...]}
"""

from _serve import run_agent


def handle(_capability: str, payload: dict) -> dict:
    questions = payload.get("questions", [])
    critique = []
    for i, q in enumerate(questions, start=1):
        if "?" not in q:
            critique.append(f"Q{i}: phrase as a direct question.")
        elif len(q) < 60:
            critique.append(f"Q{i}: ask for a specific example to make it behavioral.")
        else:
            critique.append(f"Q{i}: good — add a follow-up probing trade-offs.")
    if not critique:
        critique = ["No questions to review."]
    return {"critique": critique}


if __name__ == "__main__":
    run_agent(handle, 8003)
