"""Question Designer — capability "questions".

Input:  {"role": ..., "persona": ...}
Output: {"role": ..., "questions": ["...", ...]}
"""

from _serve import run_agent


def handle(_capability: str, payload: dict) -> dict:
    role = payload.get("role", "the role")
    questions = [
        f"Walk me through a system you designed and shipped for a {role}-type problem.",
        "Describe a production incident you handled. What was the root cause and the fix?",
        "How do you decide when to add a new service versus extend an existing one?",
        "Tell me about a time you disagreed with a technical decision. What did you do?",
        "What would you want to learn or own in your first 90 days here?",
    ]
    return {"role": role, "questions": questions}


if __name__ == "__main__":
    run_agent(handle, 8002)
