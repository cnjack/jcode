#!/usr/bin/env python3
"""Render the self-contained HTML report for the jcode agent test suite.

Inputs: analysis.json (aggregates), roundtable.json, findings.json, and the
runs directory (for showcase trajectories). Output: a single standalone
report.html with inline CSS + inline SVG charts. No external assets.
"""
import argparse
import html
import json
from datetime import datetime, timezone
from pathlib import Path

ORANGE = "#FF8400"


def esc(s):
    return html.escape(str(s), quote=True)


def load(p):
    return json.loads(Path(p).read_text())


def pct(x):
    return f"{round(x * 100)}%"


# ---------- small chart helpers ----------

def bar_row(label, k, n, color=ORANGE, width=320):
    rate = (k / n) if n else 0
    w = int(rate * width)
    return f"""
    <div class="barrow">
      <div class="barlabel">{esc(label)}</div>
      <div class="bartrack" style="width:{width}px">
        <div class="barfill" style="width:{w}px;background:{color}"></div>
      </div>
      <div class="barval">{k}/{n} · {pct(rate)}</div>
    </div>"""


def sev_chip(sev):
    colors = {"critical": "#ff4d4f", "high": "#ff7a45", "medium": "#ffc53d",
              "low": "#73d13d", "info": "#40a9ff"}
    c = colors.get(sev, "#888")
    return f'<span class="chip" style="background:{c}22;color:{c};border:1px solid {c}66">{esc(sev.upper())}</span>'


# ---------- trajectory rendering ----------

KIND_ICON = {"read": "📖", "edit": "✏️", "search": "🔎", "execute": "⚙️",
             "fetch": "🌐", "think": "💭", "other": "🔧", "switch_mode": "🔀"}


def render_trajectory(rundir):
    ev = Path(rundir) / "events.jsonl"
    if not ev.exists():
        return "<p class='muted'>no event log</p>"
    calls = {}
    order = []
    final = []
    for line in ev.read_text(errors="ignore").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            d = json.loads(line)
        except Exception:
            continue
        if d.get("kind") != "session_update":
            continue
        u = d.get("data", {})
        su = u.get("sessionUpdate")
        if su == "tool_call":
            tid = u.get("toolCallId")
            calls[tid] = {"title": u.get("title", ""), "kind": u.get("kind", "other"),
                          "input": u.get("rawInput"), "status": u.get("status", ""),
                          "output": None}
            order.append(tid)
        elif su == "tool_call_update":
            tid = u.get("toolCallId")
            if tid in calls:
                if u.get("status"):
                    calls[tid]["status"] = u["status"]
                if u.get("rawOutput") is not None:
                    calls[tid]["output"] = u["rawOutput"]
        elif su == "agent_message_chunk":
            t = (u.get("content") or {}).get("text")
            if t:
                final.append(t)
    rows = []
    for i, tid in enumerate(order, 1):
        c = calls[tid]
        icon = KIND_ICON.get(c["kind"], "🔧")
        inp = json.dumps(c["input"])[:160] if c["input"] is not None else ""
        out = c["output"]
        if isinstance(out, (dict, list)):
            out = json.dumps(out)
        out = (str(out)[:180] if out is not None else "")
        stat = c["status"]
        stat_c = {"completed": "#73d13d", "failed": "#ff4d4f", "in_progress": "#ffc53d"}.get(stat, "#888")
        rows.append(f"""
        <div class="tstep">
          <div class="tnum">{i}</div>
          <div class="tbody">
            <div class="ttitle">{icon} <b>{esc(c['title'])}</b>
              <span class="tstat" style="color:{stat_c}">{esc(stat)}</span></div>
            {f'<div class="tio">→ {esc(inp)}</div>' if inp else ''}
            {f'<div class="tio out">← {esc(out)}</div>' if out else ''}
          </div>
        </div>""")
    finaltext = esc("".join(final))[:1200]
    return f"""<div class="traj">{''.join(rows)}
      <div class="tfinal"><div class="tfinal-h">final answer</div>{finaltext}</div></div>"""


# ---------- section builders ----------

def kpi(label, value, sub=""):
    return f"""<div class="kpi"><div class="kpi-v">{esc(value)}</div>
      <div class="kpi-l">{esc(label)}</div>
      {f'<div class="kpi-s">{esc(sub)}</div>' if sub else ''}</div>"""


def build(analysis, roundtable, findings, runs_dir, meta):
    ov = analysis["overall"]
    models = analysis["models"]
    cases = analysis["cases"]
    tiers = analysis["tiers"]
    sigs = analysis["signatures"]

    # KPIs
    verdict_rate = ov.get("task_pass_rate", 0)
    kpis = "".join([
        kpi("Total runs", ov["total_runs"], f"{len(cases)} cases × {len(models)} models"),
        kpi("Task pass rate", pct(verdict_rate),
            f"95% CI {pct(ov['task_pass_ci'][0])}–{pct(ov['task_pass_ci'][1])}"),
        kpi("Clean termination", f"{ov['clean_termination']}/{ov['total_runs']}",
            "ended with end_turn"),
        kpi("Contract pass", f"{ov['contract_pass']}/{ov['total_runs']}", "ACP-level checks"),
        kpi("Tokens used", f"{ov['total_tokens']:,}",
            (f"≈ ${ov['total_cost_est']}" if ov.get("total_cost_est") else "flat-rate plan · cost n/a")),
        kpi("Defects found", len(findings["findings"]),
            f"{sum(1 for f in findings['findings'] if f['severity'] in ('critical','high'))} high/critical"),
    ])

    # model comparison
    mrows = ""
    for m, d in sorted(models.items()):
        flag = ""
        if d["nonterminal"]:
            flag = f'<span class="warn">{d["nonterminal"]} non-terminal</span>'
        mrows += f"""<tr>
          <td><b>{esc(m)}</b></td>
          <td>{d['runs']}</td>
          <td>{d['task_pass']} ({pct(d['pass_rate'])})<div class="cismall">CI {pct(d['ci'][0])}–{pct(d['ci'][1])}</div></td>
          <td>{d['clean_termination']}</td>
          <td>{d['contract_pass']}</td>
          <td>{d['avg_tool_calls']}</td>
          <td>{d['avg_wall_s']}s</td>
          <td>{d['avg_tokens']:,}</td>
          <td>{('$'+str(d['cost_est'])) if d.get('cost_est') else '—'}</td>
          <td>{flag or '·'}</td>
        </tr>"""
    model_bars = "".join(bar_row(m, d["task_pass"], d["runs"]) for m, d in sorted(models.items()))
    tier_bars = "".join(bar_row(t, d["pass"], d["n"],
                                color={"smoke": "#40a9ff", "core": ORANGE,
                                       "stress": "#ff7a45", "safety": "#9254de"}.get(t, ORANGE))
                        for t, d in sorted(tiers.items()))

    # case matrix
    model_names = sorted(models.keys())
    head = "".join(f"<th>{esc(m)}</th>" for m in model_names)
    crows = ""
    for cid, d in sorted(cases.items(), key=lambda x: (x[1]["tier"], x[0])):
        cells = ""
        for m in model_names:
            bm = d["by_model"].get(m)
            if not bm:
                cells += "<td class='cell na'>—</td>"
            else:
                ok = bm["pass"] == bm["n"]
                zero = bm["pass"] == 0
                cls = "cell pass" if ok else ("cell fail" if zero else "cell partial")
                cells += f"<td class='{cls}'>{bm['pass']}/{bm['n']}</td>"
        flaky = '<span class="flaky">flaky</span>' if d["flaky"] else ""
        crows += f"""<tr>
          <td class="ccase"><span class="tier tier-{esc(d['tier'])}">{esc(d['tier'])}</span>
            {esc(d['title'])} {flaky}<div class="muted">{esc(d['category'])} · avg {d['avg_tool_calls']} tools</div></td>
          {cells}
        </tr>"""

    # round table
    seats = ""
    for s in roundtable["seats"]:
        pts = "".join(f"<li>{esc(p)}</li>" for p in s["points"])
        seats += f"""<div class="seat">
          <div class="seat-role">{esc(s['role'])}</div>
          <div class="seat-thesis">“{esc(s['thesis'])}”</div>
          <ul>{pts}</ul></div>"""
    synth = "".join(f"<li>{esc(x)}</li>" for x in roundtable["synthesis"])

    # findings
    fcards = ""
    for f in findings["findings"]:
        ev = "".join(f"<li>{esc(e)}</li>" for e in f["evidence"])
        fcards += f"""<div class="finding">
          <div class="finding-h">{sev_chip(f['severity'])}
            <span class="fid">{esc(f['id'])}</span>
            <span class="ftitle">{esc(f['title'])}</span>
            <span class="fsurface">{esc(f['surface'])}</span></div>
          <p class="fsummary">{esc(f['summary'])}</p>
          <div class="fgrid">
            <div><div class="flabel">Evidence</div><ul>{ev}</ul></div>
            <div>
              <div class="flabel">Root cause</div><p>{esc(f['root_cause'])}</p>
              <div class="flabel">Impact</div><p>{esc(f['impact'])}</p>
              <div class="flabel">Recommendation</div><p class="frec">{esc(f['recommendation'])}</p>
            </div>
          </div></div>"""

    # showcases
    prefer = ["stress_long_horizon__glm-5.1__r1", "safety_prompt_injection__glm-5.1__r1",
              "stress_impossible__glm-5.1__r1", "core_bugfix_failing_test__glm-5.1__r1",
              "core_multifile_refactor__glm-5.1__r1"]
    showcases = ""
    shown = 0
    for rid in prefer:
        rd = Path(runs_dir) / rid
        rec_p = rd / "record.json"
        if not rec_p.exists() or shown >= 4:
            continue
        rec = load(rec_p)
        verdict = "PASS" if rec.get("task_passed") else "FAIL"
        vc = "#73d13d" if rec.get("task_passed") else "#ff4d4f"
        showcases += f"""<div class="showcase">
          <div class="sc-h"><span class="sc-verdict" style="background:{vc}22;color:{vc};border:1px solid {vc}66">{verdict}</span>
            <b>{esc(rec.get('case_title'))}</b>
            <span class="muted">{esc(rec.get('model'))} · stop={esc(rec.get('stop_reason'))} · {rec.get('tool_calls')} tools · {rec.get('wall_s')}s · {rec.get('usage_total',{}).get('total',0):,} tok</span></div>
          <div class="sc-prompt">▸ {esc(rec.get('prompt'))[:400]}</div>
          {render_trajectory(rd)}</div>"""
        shown += 1

    # signatures summary
    sig_items = "".join([
        f"<li><b>{len(sigs['non_termination'])}</b> non-terminal runs (timeout / abnormal stop)</li>",
        f"<li><b>{len(sigs['silent_empty_turn'])}</b> silent empty turns (end_turn with no output)</li>",
        f"<li><b>{len(sigs['tool_loop_suspects'])}</b> tool-loop suspects (≥3 identical calls)</li>",
        f"<li><b>{sigs['usage_absent_pct']}%</b> of runs reported no usage on the ACP stream</li>",
        f"<li><b>{len(sigs['contract_violations'])}</b> runs with an ACP contract violation</li>",
    ])

    css = """
    :root{--bg:#0d0f12;--panel:#15181d;--panel2:#1b1f26;--line:#282d36;--txt:#e6e9ef;--muted:#8b93a3;--orange:#FF8400}
    *{box-sizing:border-box}
    body{margin:0;background:var(--bg);color:var(--txt);font:15px/1.6 -apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif}
    .wrap{max-width:1100px;margin:0 auto;padding:0 24px 80px}
    code,.mono{font-family:'SF Mono',ui-monospace,Menlo,Monaco,'Cascadia Code',monospace}
    header.top{padding:56px 0 34px;border-bottom:1px solid var(--line);margin-bottom:34px}
    .brand{font-family:ui-monospace,Menlo,monospace;font-weight:700;font-size:15px;letter-spacing:.06em;color:var(--muted)}
    .brand b{color:var(--orange)}
    h1{font-size:38px;margin:12px 0 8px;letter-spacing:-.02em;line-height:1.15}
    h1 .accent{color:var(--orange)}
    .subtitle{color:var(--muted);font-size:17px;max-width:760px}
    .metaline{margin-top:18px;color:var(--muted);font-size:13px;display:flex;flex-wrap:wrap;gap:8px 18px}
    .metaline b{color:var(--txt);font-weight:600}
    h2{font-size:13px;text-transform:uppercase;letter-spacing:.14em;color:var(--orange);margin:52px 0 6px;font-weight:700}
    h2 .n{color:var(--muted);margin-right:8px}
    .lead{color:var(--muted);margin:0 0 20px;max-width:820px}
    .kpis{display:grid;grid-template-columns:repeat(6,1fr);gap:12px}
    @media(max-width:900px){.kpis{grid-template-columns:repeat(3,1fr)}.kpis2{grid-template-columns:1fr!important}}
    .kpi{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:16px}
    .kpi-v{font-size:26px;font-weight:700;letter-spacing:-.02em}
    .kpi-l{color:var(--muted);font-size:12px;text-transform:uppercase;letter-spacing:.06em;margin-top:4px}
    .kpi-s{color:var(--muted);font-size:12px;margin-top:6px;opacity:.8}
    .panel{background:var(--panel);border:1px solid var(--line);border-radius:14px;padding:22px;margin-top:16px}
    .verdict{background:linear-gradient(180deg,#1b1f26,#15181d);border:1px solid var(--line);border-left:3px solid var(--orange);border-radius:12px;padding:20px 22px;margin-top:18px}
    table{width:100%;border-collapse:collapse;font-size:14px;margin-top:8px}
    th,td{text-align:left;padding:9px 10px;border-bottom:1px solid var(--line);vertical-align:top}
    th{color:var(--muted);font-weight:600;font-size:12px;text-transform:uppercase;letter-spacing:.05em}
    tr:hover td{background:#ffffff05}
    .cismall,.muted{color:var(--muted);font-size:12px}
    .warn{color:#ff7a45;font-size:12px;font-weight:600}
    .barrow{display:flex;align-items:center;gap:12px;margin:7px 0}
    .barlabel{width:120px;color:var(--muted);font-size:13px;text-align:right}
    .bartrack{height:16px;background:#ffffff0a;border-radius:8px;overflow:hidden}
    .barfill{height:100%;border-radius:8px}
    .barval{font-size:13px;color:var(--txt);min-width:96px}
    .cell{text-align:center;font-weight:600;font-size:13px}
    .cell.pass{background:#73d13d18;color:#73d13d}
    .cell.fail{background:#ff4d4f18;color:#ff4d4f}
    .cell.partial{background:#ffc53d18;color:#ffc53d}
    .cell.na{color:var(--muted)}
    .ccase{max-width:420px}
    .tier{display:inline-block;font-size:10px;text-transform:uppercase;letter-spacing:.06em;padding:2px 6px;border-radius:5px;margin-right:6px;font-weight:700}
    .tier-smoke{background:#40a9ff22;color:#40a9ff}.tier-core{background:#FF840022;color:#FF8400}
    .tier-stress{background:#ff7a4522;color:#ff7a45}.tier-safety{background:#9254de22;color:#9254de}
    .flaky{background:#ffc53d22;color:#ffc53d;font-size:10px;padding:1px 6px;border-radius:5px;text-transform:uppercase;letter-spacing:.05em}
    .seats{display:grid;grid-template-columns:1fr 1fr;gap:14px}
    @media(max-width:820px){.seats{grid-template-columns:1fr}.fgrid{grid-template-columns:1fr!important}}
    .seat{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:16px 18px}
    .seat-role{color:var(--orange);font-weight:700;font-size:14px}
    .seat-thesis{color:var(--txt);font-style:italic;margin:6px 0 8px;font-size:13.5px}
    .seat ul,.finding ul{margin:6px 0 0;padding-left:18px}
    .seat li{color:var(--muted);font-size:13px;margin:4px 0}
    .chip{font-size:10px;font-weight:700;padding:2px 8px;border-radius:6px;letter-spacing:.05em}
    .finding{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:18px 20px;margin-top:14px}
    .finding-h{display:flex;align-items:center;gap:10px;flex-wrap:wrap}
    .fid{color:var(--muted);font-family:ui-monospace,monospace;font-weight:700}
    .ftitle{font-weight:700;font-size:15.5px;flex:1;min-width:240px}
    .fsurface{color:var(--muted);font-size:11px;text-transform:uppercase;letter-spacing:.06em;border:1px solid var(--line);padding:2px 7px;border-radius:6px}
    .fsummary{color:var(--txt);margin:12px 0}
    .fgrid{display:grid;grid-template-columns:1fr 1fr;gap:18px;margin-top:8px}
    .flabel{color:var(--orange);font-size:11px;text-transform:uppercase;letter-spacing:.08em;margin:10px 0 4px;font-weight:700}
    .finding p{margin:2px 0;color:var(--muted);font-size:13.5px}
    .finding li{color:var(--muted);font-size:13px;margin:4px 0}
    .frec{color:var(--txt)!important}
    .showcase{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:16px 18px;margin-top:14px}
    .sc-h{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-bottom:8px}
    .sc-verdict{font-size:11px;font-weight:700;padding:2px 9px;border-radius:6px}
    .sc-prompt{color:var(--muted);font-size:13px;font-style:italic;border-left:2px solid var(--line);padding-left:10px;margin-bottom:12px}
    .traj{display:flex;flex-direction:column;gap:8px}
    .tstep{display:flex;gap:10px;align-items:flex-start}
    .tnum{width:22px;height:22px;flex:none;background:#ffffff0a;border:1px solid var(--line);border-radius:50%;display:flex;align-items:center;justify-content:center;font-size:11px;color:var(--muted)}
    .tbody{flex:1;min-width:0}
    .ttitle{font-size:13.5px}
    .tstat{font-size:11px;margin-left:8px;text-transform:uppercase;letter-spacing:.04em}
    .tio{font-family:ui-monospace,Menlo,monospace;font-size:11.5px;color:var(--muted);background:#ffffff06;border-radius:6px;padding:4px 8px;margin-top:3px;overflow-x:auto;white-space:pre-wrap;word-break:break-word}
    .tio.out{color:#8fb98f}
    .tfinal{margin-top:6px;background:#ffffff06;border-radius:8px;padding:10px 12px;font-size:13px}
    .tfinal-h{color:var(--orange);font-size:10px;text-transform:uppercase;letter-spacing:.08em;margin-bottom:4px}
    .siglist li{margin:6px 0}
    footer{margin-top:60px;padding-top:20px;border-top:1px solid var(--line);color:var(--muted);font-size:12px}
    .pipe{display:flex;gap:8px;flex-wrap:wrap;align-items:center;margin-top:8px}
    .pnode{background:var(--panel2);border:1px solid var(--line);border-radius:8px;padding:8px 12px;font-size:12.5px}
    .parr{color:var(--orange)}
    """

    findings_high = [f for f in findings["findings"] if f["severity"] in ("critical", "high")]
    verdict_text = (
        f"Across <b>{ov['total_runs']} unattended runs</b>, jcode completed <b>{pct(verdict_rate)}</b> "
        f"of tasks with deterministic verification, and <b>{ov['clean_termination']}/{ov['total_runs']}</b> "
        f"terminated cleanly. The agent's task competence on well-specified work is strong — but the suite "
        f"surfaced <b>{len(findings['findings'])} defects</b>, including <b>{len(findings_high)} high/critical</b> "
        f"that would block a dependable SDK: a build that crashes on fork, model errors masked as success, "
        f"unbounded non-termination, and an unenforced filesystem/exec boundary. "
        f"The good news: every one is specific, reproducible, and fixable."
    )

    return f"""<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>JCODE Agent — Autonomous Execution Test Report</title>
<style>{css}</style></head><body><div class="wrap">
<header class="top">
  <div class="brand">[<b>J</b>CODE] · AGENT RELIABILITY EVALUATION</div>
  <h1>Autonomous Execution <span class="accent">Test Report</span></h1>
  <div class="subtitle">An unattended, fully-automated evaluation of jcode's coding agent, driven through its
  headless ACP interface — the same surface a future SDK will build on. Every result is verified against the
  sandbox end-state, never the agent's self-report.</div>
  <div class="metaline">
    <span>Generated <b>{esc(meta['date'])}</b></span>
    <span>Binary <b>{esc(meta['binary'])}</b></span>
    <span>Models <b>{esc(', '.join(sorted(models.keys())))}</b></span>
    <span>Host <b>{esc(meta['host'])}</b></span>
    <span>Runs <b>{ov['total_runs']}</b></span>
  </div>
</header>

<h2><span class="n">01</span>Executive summary</h2>
<div class="kpis">{kpis}</div>
<div class="verdict">{verdict_text}</div>

<h2><span class="n">02</span>The round table — how we decided to test</h2>
<p class="lead">{esc(roundtable['premise'])}</p>
<div class="seats">{seats}</div>
<div class="panel"><div class="flabel">Synthesis → test design</div><ul>{synth}</ul></div>

<h2><span class="n">03</span>Methodology</h2>
<div class="panel">
  <p class="lead">Each run is doubly isolated and judged deterministically:</p>
  <div class="pipe">
    <span class="pnode">throwaway <b>HOME</b> (real keys, pinned model)</span><span class="parr">+</span>
    <span class="pnode">throwaway <b>sandbox cwd</b> (fixtures + canary)</span><span class="parr">→</span>
    <span class="pnode"><b>ACP harness</b> drives one prompt turn</span><span class="parr">→</span>
    <span class="pnode">records full <b>event stream</b></span><span class="parr">→</span>
    <span class="pnode"><b>oracles</b> + <b>contract checks</b></span>
  </div>
  <ul>
    <li><b>Isolation:</b> an isolated HOME means the agent can never touch the operator's real ~/.jcode; an isolated cwd + a canary just outside it bounds and detects filesystem escape.</li>
    <li><b>Deterministic oracles:</b> file bytes, subprocess exit codes/stdout, grep over the tree, mutation checks (does the authored test actually fail on a broken impl?), and read-only-discipline (no fixture changed). We never trust the agent saying "done".</li>
    <li><b>Contract checks</b> on every run: exactly one terminal StopReason, no orphaned tool calls, pure-protocol stdout, usage reported.</li>
    <li><b>Stability:</b> cases repeat across models; we report pass@n, flakiness, and Wilson 95% CIs.</li>
  </ul>
</div>

<h2><span class="n">04</span>Results by model</h2>
<div class="panel">
  <table><thead><tr><th>Model</th><th>Runs</th><th>Task pass</th><th>Clean end</th><th>Contract</th>
   <th>Avg tools</th><th>Avg wall</th><th>Avg tokens</th><th>Cost est</th><th>Flags</th></tr></thead>
   <tbody>{mrows}</tbody></table>
  <div style="margin-top:18px">{model_bars}</div>
</div>

<h2><span class="n">05</span>Results by difficulty tier</h2>
<div class="panel">{tier_bars}</div>

<h2><span class="n">06</span>Case × model matrix</h2>
<div class="panel"><table><thead><tr><th>Case</th>{head}</tr></thead><tbody>{crows}</tbody></table></div>

<h2><span class="n">07</span>Log analysis — failure signatures</h2>
<div class="panel"><ul class="siglist">{sig_items}</ul></div>

<h2><span class="n">08</span>Defects found</h2>
<p class="lead">Ranked by severity. Each is reproducible from the recorded runs.</p>
{fcards}

<h2><span class="n">09</span>Showcase trajectories</h2>
<p class="lead">Real recorded runs — the full tool-by-tool trajectory the agent took, straight from the ACP event stream.</p>
{showcases}

<footer>
  Generated by the jcode agent-eval harness · deterministic verification over {ov['total_runs']} isolated runs ·
  models {esc(', '.join(sorted(models.keys())))} · all artifacts under <span class="mono">agent-eval/</span>.
</footer>
</div></body></html>"""


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--analysis", required=True)
    ap.add_argument("--roundtable", required=True)
    ap.add_argument("--findings", required=True)
    ap.add_argument("--runs-dir", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--binary", default="jcode (CGO_ENABLED=0)")
    ap.add_argument("--host", default="macOS 26 (Darwin 25.5.0), arm64")
    args = ap.parse_args()

    analysis = load(args.analysis)
    roundtable = load(args.roundtable)
    findings = load(args.findings)
    meta = {"date": datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC"),
            "binary": args.binary, "host": args.host}
    htmlout = build(analysis, roundtable, findings, args.runs_dir, meta)
    Path(args.out).write_text(htmlout)
    print(f"wrote {args.out} ({len(htmlout):,} bytes)")


if __name__ == "__main__":
    main()
