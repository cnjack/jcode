#!/usr/bin/env python3
"""Build the showcase from a completed frontend batch.

Reads runs/all_records.json (the most recent orchestrate.py run), and for each
frontend case picks the best artifact to display: prefer a passing run, prefer
glm-5.1, tie-break on larger artifact (richer output). Copies that run's
index.html into showcase/projects/<case_id>/ and writes showcase/data.json with
per-project metadata + aggregate metrics.

Run after: orchestrate.py --tiers frontend ...
"""
import json
import shutil
from pathlib import Path

HERE = Path(__file__).resolve().parent          # agent-eval/showcase
EVAL = HERE.parent                              # agent-eval
RUNS = EVAL / "runs"
PROJECTS = HERE / "projects"

# display metadata for each frontend case (order = display order)
META = {
    "fe_landing_hero": {
        "title": "Product Landing Page",
        "description": "Marketing hero for a fictional dev tool — nav, CTAs, feature cards.",
    },
    "fe_analytics_dashboard": {
        "title": "Analytics Dashboard",
        "description": "Dark KPI dashboard with inline-SVG bar and line charts.",
    },
    "fe_todo_app": {
        "title": "To-Do App",
        "description": "Vanilla-JS task list with filters and localStorage persistence.",
    },
    "fe_pricing_calculator": {
        "title": "Pricing Calculator",
        "description": "Live seat slider + monthly/annual toggle with reactive totals.",
    },
    "fe_canvas_particles": {
        "title": "Canvas Particle Hero",
        "description": "Animated particle network on <canvas> with requestAnimationFrame.",
    },
    "fe_svg_dataviz": {
        "title": "SVG Donut Chart",
        "description": "Browser-share donut chart with legend, drawn as inline SVG.",
    },
}

MODEL_RANK = {"glm-5.1": 0, "glm-5.2": 1, "qwen3.5-flash": 2}


def artifact_path(run_id):
    return RUNS / run_id / "work" / "box" / "index.html"


def score(rec):
    """Lower is better: passing first, then preferred model, then bigger file."""
    passed = 0 if rec.get("task_passed") else 1
    model = MODEL_RANK.get(rec.get("model"), 9)
    ap = artifact_path(rec["run_id"])
    size = ap.stat().st_size if ap.exists() else 0
    exists = 0 if ap.exists() else 1
    return (exists, passed, model, -size)


def main():
    records = json.loads((RUNS / "all_records.json").read_text())
    fe = [r for r in records if r.get("tier") == "frontend"]
    if not fe:
        print("no frontend records in all_records.json — run the batch first")
        return

    runs_n = len(fe)
    pass_n = sum(1 for r in fe if r.get("task_passed"))

    PROJECTS.mkdir(parents=True, exist_ok=True)
    projects = []
    for case_id, meta in META.items():
        cands = [r for r in fe if r["case_id"] == case_id]
        if not cands:
            continue
        best = min(cands, key=score)
        src = artifact_path(best["run_id"])
        dest_dir = PROJECTS / case_id
        dest_dir.mkdir(parents=True, exist_ok=True)
        copied = False
        if src.exists():
            shutil.copy(src, dest_dir / "index.html")
            copied = True
        projects.append({
            "id": case_id,
            "title": meta["title"],
            "description": meta["description"],
            "model": best.get("model"),
            "passed": bool(best.get("task_passed")),
            "wall_s": best.get("wall_s"),
            "tokens": (best.get("usage_total") or {}).get("total", 0),
            "bytes": (src.stat().st_size if src.exists() else 0),
            "run_id": best["run_id"],
            "artifact_copied": copied,
        })

    data = {
        "runs": runs_n,
        "pass": pass_n,
        "rate": f"{round(100 * pass_n / runs_n)}%",
        "projects": projects,
    }
    (HERE / "data.json").write_text(json.dumps(data, indent=2))
    print(f"wrote data.json: {len(projects)} projects, {pass_n}/{runs_n} passed")
    for p in projects:
        flag = "ok" if p["artifact_copied"] else "MISSING"
        print(f"  {p['id']:26s} {p['model']:9s} pass={p['passed']!s:5s} "
              f"{p['bytes']:6d}B {flag}")


if __name__ == "__main__":
    main()
