"""Compiler — capability "compile".

Input:  {"role", "persona", "questions": [...], "critique": [...]}
Output: {"role", "guide_markdown": "<final markdown>"}
"""

from _serve import run_agent


def handle(_capability: str, payload: dict) -> dict:
    role = payload.get("role", "the role")
    persona = payload.get("persona", "")
    questions = payload.get("questions", [])
    critique = payload.get("critique", [])

    lines = [f"# Interview Guide — {role}", ""]
    if persona:
        lines += ["## Candidate persona", "", persona, ""]

    lines += ["## Questions", ""]
    for i, q in enumerate(questions, start=1):
        lines.append(f"{i}. {q}")
    lines.append("")

    if critique:
        lines += ["## Reviewer notes", ""]
        lines += [f"- {c}" for c in critique]
        lines.append("")

    return {"role": role, "guide_markdown": "\n".join(lines)}


if __name__ == "__main__":
    run_agent(handle, 8004)
