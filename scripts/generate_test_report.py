#!/usr/bin/env python3
"""
generate_test_report.py
Consolidated HTML test and coverage report for cb-event-forwarder.

Reads (all arguments are optional; missing files are handled gracefully):
  --unit-junit      JUnit XML produced by go-junit-report for unit tests
  --unit-coverage   go tool cover -func output for unit tests
  --unit-func-pct   avg function coverage percent (single float) for unit tests
  --int-junit       JUnit XML for integration tests
  --int-coverage    go tool cover -func output for integration tests
  --int-func-pct    avg function coverage percent for integration tests
  --os-label        OS label shown in the report header (e.g. EL8, EL9)
  --build-number    Jenkins build number
  --output          Destination HTML file (default: build/test-report/report.html)

Usage:
    python3 scripts/generate_test_report.py \\
        --unit-junit   build/test-results/junit.xml \\
        --unit-coverage build/code_coverage/unittest_coverage.txt \\
        --unit-func-pct build/code_coverage/function_coverage_percent.txt \\
        --int-junit    build/integration-test-results/junit.xml \\
        --int-coverage build/integration-test-results/integration_coverage.txt \\
        --int-func-pct build/integration-test-results/function_coverage_percent.txt \\
        --os-label EL8 --build-number 42 \\
        --output build/test-report/report.html
"""
import argparse
import html as _html
import os
import re
import sys
import xml.etree.ElementTree as ET
from datetime import datetime

# ─── File helpers ─────────────────────────────────────────────────────────────

def slurp(path):
    if not path or not os.path.exists(path):
        return None
    try:
        with open(path, encoding="utf-8", errors="replace") as fh:
            return fh.read()
    except OSError:
        return None


# ─── Parsers ──────────────────────────────────────────────────────────────────

def parse_junit(path):
    """Return {'totals': {...}, 'suites': [...]} or None."""
    text = slurp(path)
    if not text:
        return None
    try:
        root = ET.fromstring(text)
    except ET.ParseError as exc:
        print(f"Warning: could not parse {path}: {exc}", file=sys.stderr)
        return None

    if root.tag == "testsuites":
        suite_els = root.findall("testsuite")
    elif root.tag == "testsuite":
        suite_els = [root]
    else:
        suite_els = root.findall(".//testsuite")

    suites = []
    for s in suite_els:
        cases = []
        for tc in s.findall("testcase"):
            fail_el = tc.find("failure")
            err_el  = tc.find("error")
            skip_el = tc.find("skipped")
            if fail_el is not None:
                status  = "FAIL"
                message = (fail_el.get("message") or fail_el.text or "").strip()
            elif err_el is not None:
                status  = "ERROR"
                message = (err_el.get("message") or err_el.text or "").strip()
            elif skip_el is not None:
                status  = "SKIP"
                message = (skip_el.get("message") or "").strip()
            else:
                status  = "PASS"
                message = ""
            cases.append({
                "name":   tc.get("name", ""),
                "time":   float(tc.get("time") or 0),
                "status": status,
                "msg":    message[:800],
            })
        suites.append({
            "name":     s.get("name", ""),
            "tests":    int(s.get("tests",    0) or 0),
            "failures": int(s.get("failures", 0) or 0),
            "errors":   int(s.get("errors",   0) or 0),
            "skipped":  int(s.get("skipped",  0) or 0),
            "time":     float(s.get("time",   0) or 0),
            "cases":    cases,
        })

    totals = {k: sum(s[k] for s in suites)
              for k in ("tests", "failures", "errors", "skipped", "time")}
    totals["passed"] = max(
        0, totals["tests"] - totals["failures"] - totals["errors"] - totals["skipped"]
    )
    return {"totals": totals, "suites": suites}


def parse_cover_func(path):
    """
    Parse 'go tool cover -func' output.
    Returns {'total_stmt': float|None, 'packages': [{'pkg', 'funcs': [...]}]}
    or None if file is missing.
    """
    text = slurp(path)
    if not text:
        return None
    total_stmt = None
    pkgs: dict = {}
    for line in text.splitlines():
        m = re.match(r"^total:\s+\(statements\)\s+([\d.]+)%", line)
        if m:
            total_stmt = float(m.group(1))
            continue
        m = re.match(r"^(\S+):(\d+):\s+(\S+)\s+([\d.]+)%", line)
        if m:
            fpath, func, pct = m.group(1), m.group(3), float(m.group(4))
            parts = fpath.split("/")
            pkg   = "/".join(parts[:-1]) if len(parts) > 1 else fpath
            fname = parts[-1]
            pkgs.setdefault(pkg, []).append({"file": fname, "func": func, "pct": pct})
    return {
        "total_stmt": total_stmt,
        "packages":   [{"pkg": k, "funcs": v} for k, v in sorted(pkgs.items())],
    }


def read_float(path):
    t = slurp(path)
    if t is None:
        return None
    try:
        return float(t.strip())
    except ValueError:
        return None


# ─── HTML helpers ─────────────────────────────────────────────────────────────

def esc(s):
    return _html.escape(str(s))


def pct_tier(pct):
    if pct is None:
        return "neutral"
    if pct >= 80:
        return "green"
    if pct >= 60:
        return "orange"
    return "red"


_TIER_CLR = {"green": "#28a745", "orange": "#fd7e14", "red": "#dc3545", "neutral": "#adb5bd"}


def pct_bar_html(pct, bar_width=110):
    if pct is None:
        return '<span class="na">—</span>'
    clr = _TIER_CLR[pct_tier(pct)]
    filled = int(min(100, max(0, pct)) * bar_width / 100)
    return (
        f'<div class="pct-cell">'
        f'<span class="pct-val" style="color:{clr}">{pct:.1f}%</span>'
        f'<div class="bar-bg" style="width:{bar_width}px">'
        f'<div class="bar-fill" style="width:{filled}px;background:{clr}"></div>'
        f'</div></div>'
    )


def status_badge(status):
    cls = {"PASS": "bp", "FAIL": "bf", "ERROR": "be", "SKIP": "bs"}.get(status, "bp")
    icon = {"PASS": "✓", "FAIL": "✗", "ERROR": "✗", "SKIP": "⊘"}.get(status, "")
    return f'<span class="badge {cls}">{icon}&nbsp;{esc(status)}</span>'


# ─── Section renderers ────────────────────────────────────────────────────────

def _card(title, body_html):
    return f'<div class="card"><h3 class="card-title">{esc(title)}</h3>{body_html}</div>'


def summary_cards_html(unit_junit, unit_cov, unit_fpct,
                       int_junit,  int_cov,  int_fpct):
    cards = []
    for label, junit_d, cov_d, fpct in [
        ("Unit Tests",        unit_junit, unit_cov, unit_fpct),
        ("Integration Tests", int_junit,  int_cov,  int_fpct),
    ]:
        if junit_d is None and cov_d is None:
            cards.append(_card(label, '<span class="na">Not run this build</span>'))
            continue

        body = []
        if junit_d:
            t = junit_d["totals"]
            failed = t["failures"] + t["errors"]
            color  = "green" if failed == 0 and t["tests"] > 0 else "red" if failed > 0 else "neutral"
            clr    = _TIER_CLR[color]
            body.append(
                f'<div class="big-num" style="color:{clr}">'
                f'{t["passed"]}<span class="big-denom">/{t["tests"]}</span></div>'
                f'<div class="num-sub">'
                f'<span class="badge bp">✓&nbsp;{t["passed"]} passed</span>'
                + (f'<span class="badge bf">✗&nbsp;{failed} failed</span>' if failed else "")
                + (f'<span class="badge bs">⊘&nbsp;{t["skipped"]} skipped</span>' if t["skipped"] else "")
                + f'&nbsp;<span class="muted">{t["time"]:.1f}s</span></div>'
            )

        if cov_d and cov_d.get("total_stmt") is not None:
            stmt = cov_d["total_stmt"]
            body.append(f'<div class="cov-row">{pct_bar_html(stmt, 90)}'
                        f'<span class="muted" style="margin-left:6px;font-size:0.78rem">statements</span></div>')

        if fpct is not None:
            body.append(f'<div class="cov-row muted" style="font-size:0.78rem">'
                        f'{fpct:.1f}% avg function coverage</div>')

        cards.append(_card(label, "".join(body)))

    return '<div class="cards">' + "".join(cards) + '</div>'


def test_table_html(junit_d, sec_id, title, open_by_default=True):
    """Collapsible section with per-test-case table."""
    open_attr = " open" if open_by_default else ""

    if junit_d is None:
        return (
            f'<details class="section"{open_attr}>'
            f'<summary class="sec-hdr"><span>{esc(title)}</span>'
            f'<span class="toggle-icon">▶</span></summary>'
            f'<p class="not-run">Not run this build.</p></details>\n'
        )

    t = junit_d["totals"]
    failed = t["failures"] + t["errors"]
    badges = (
        f'<span class="badge bp">✓&nbsp;{t["passed"]} passed</span> '
        + (f'<span class="badge bf">✗&nbsp;{failed} failed</span> ' if failed else "")
        + (f'<span class="badge bs">⊘&nbsp;{t["skipped"]} skipped</span>' if t["skipped"] else "")
        + f' <span class="muted">{t["time"]:.1f}s</span>'
    )

    rows = []
    for suite in junit_d["suites"]:
        for case in suite["cases"]:
            msg_html = ""
            if case["msg"]:
                msg_html = f'<div class="detail-msg">{esc(case["msg"])}</div>'
            rows.append(
                f'<tr>'
                f'<td class="test-name">{esc(case["name"])}{msg_html}</td>'
                f'<td>{status_badge(case["status"])}</td>'
                f'<td class="dur">{case["time"]:.3f}s</td>'
                f'</tr>'
            )

    table = (
        '<div class="tbl-wrap"><table>'
        '<thead><tr><th>Test</th><th>Status</th><th style="text-align:right">Duration</th></tr></thead>'
        '<tbody>' + "".join(rows) + '</tbody></table></div>'
    ) if rows else '<p class="not-run">No test cases found.</p>'

    return (
        f'<details class="section"{open_attr}>'
        f'<summary class="sec-hdr">'
        f'<span>{esc(title)}&nbsp;&nbsp;{badges}</span>'
        f'<span class="toggle-icon">▶</span></summary>'
        f'{table}</details>\n'
    )


def coverage_section_html(cov_d, fpct, sec_id, title):
    """Collapsible section with per-package / per-function coverage table."""
    if cov_d is None:
        return (
            f'<details class="section">'
            f'<summary class="sec-hdr"><span>{esc(title)}</span>'
            f'<span class="toggle-icon">▶</span></summary>'
            f'<p class="not-run">Coverage data not available.</p></details>\n'
        )

    total_stmt = cov_d.get("total_stmt")
    packages   = cov_d.get("packages", [])

    sub = ""
    if total_stmt is not None:
        sub = (f'&nbsp;&nbsp;<span class="badge" style="background:#e9ecef;color:#495057">'
               f'{total_stmt:.1f}% statements</span>')
    if fpct is not None:
        sub += (f'&nbsp;<span class="badge" style="background:#e9ecef;color:#495057">'
                f'{fpct:.1f}% avg functions</span>')

    rows = []
    for idx, pkg_info in enumerate(packages):
        pkg_name = pkg_info["pkg"]
        funcs    = pkg_info["funcs"]
        pkg_avg  = sum(f["pct"] for f in funcs) / len(funcs) if funcs else 0.0
        pkg_id   = f"{sec_id}-pkg-{idx}"

        rows.append(
            f'<tr class="pkg-row" onclick="togglePkg(\'{pkg_id}\')">'
            f'<td class="pkg-name" colspan="2">'
            f'<span id="{pkg_id}-arrow" class="pkg-arrow">▶</span> {esc(pkg_name)}</td>'
            f'<td>{pct_bar_html(pkg_avg)}</td>'
            f'</tr>'
        )
        for fn in funcs:
            rows.append(
                f'<tr class="func-row" id="{pkg_id}" style="display:none">'
                f'<td style="padding-left:2.5rem;font-family:monospace;font-size:0.8rem">'
                f'{esc(fn["file"])}</td>'
                f'<td style="font-family:monospace;font-size:0.8rem">{esc(fn["func"])}</td>'
                f'<td>{pct_bar_html(fn["pct"])}</td>'
                f'</tr>'
            )

    table = (
        '<div class="tbl-wrap"><table>'
        '<thead><tr><th>Package / File</th><th>Function</th>'
        '<th style="width:180px">Coverage</th></tr></thead>'
        '<tbody>' + "".join(rows) + '</tbody></table></div>'
    ) if rows else '<p class="not-run">No coverage data found.</p>'

    return (
        f'<details class="section">'
        f'<summary class="sec-hdr">'
        f'<span>{esc(title)}{sub}</span>'
        f'<span class="toggle-icon">▶</span></summary>'
        f'{table}</details>\n'
    )


# ─── CSS ──────────────────────────────────────────────────────────────────────

_CSS = """
*,*::before,*::after{box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Oxygen,sans-serif;
  background:#f4f6f9;color:#343a40;margin:0;font-size:14px;line-height:1.5}
a{color:#0d6efd}
header{background:linear-gradient(135deg,#1a2a3b 0%,#243447 100%);
  color:#e8edf2;padding:22px 32px 18px}
header h1{margin:0 0 4px;font-size:1.5rem;font-weight:700;letter-spacing:-.01em}
header p{margin:0;font-size:.82rem;opacity:.7}
.container{max-width:1280px;margin:0 auto;padding:24px 32px}
.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));
  gap:16px;margin-bottom:24px}
.card{background:#fff;border-radius:10px;border:1px solid #dee2e6;
  padding:20px 22px;box-shadow:0 1px 4px rgba(0,0,0,.06)}
.card-title{margin:0 0 10px;font-size:.75rem;font-weight:700;text-transform:uppercase;
  letter-spacing:.07em;color:#6c757d}
.big-num{font-size:2.4rem;font-weight:800;line-height:1.1}
.big-denom{font-size:1.1rem;font-weight:400;color:#adb5bd}
.num-sub{font-size:.78rem;margin-top:6px;display:flex;flex-wrap:wrap;gap:4px;
  align-items:center}
.cov-row{margin-top:8px;display:flex;align-items:center;flex-wrap:wrap;gap:4px}
.muted{color:#6c757d}
.na{color:#adb5bd;font-style:italic}
.not-run{padding:18px 20px;color:#6c757d;font-style:italic;margin:0}
/* badges */
.badge{display:inline-flex;align-items:center;padding:2px 7px;border-radius:10px;
  font-size:.72rem;font-weight:700;white-space:nowrap}
.bp{background:#d1f0d8;color:#155724}
.bf{background:#f8d7da;color:#721c24}
.be{background:#f8d7da;color:#721c24}
.bs{background:#fff3cd;color:#856404}
/* sections */
.section{background:#fff;border-radius:10px;border:1px solid #dee2e6;
  margin-bottom:20px;overflow:hidden;box-shadow:0 1px 4px rgba(0,0,0,.06)}
.sec-hdr{display:flex;justify-content:space-between;align-items:center;
  padding:13px 20px;background:#f8f9fa;border-bottom:1px solid #dee2e6;
  cursor:pointer;list-style:none;user-select:none;font-weight:600;font-size:.95rem}
.sec-hdr::-webkit-details-marker{display:none}
details[open]>.sec-hdr .toggle-icon{transform:rotate(90deg)}
.toggle-icon{transition:transform .18s;font-size:.8rem;color:#adb5bd;flex-shrink:0}
/* tables */
.tbl-wrap{overflow-x:auto}
table{width:100%;border-collapse:collapse;font-size:.84rem}
th{background:#f8f9fa;padding:9px 14px;text-align:left;font-weight:600;
  border-bottom:2px solid #dee2e6;color:#6c757d;font-size:.75rem;
  text-transform:uppercase;letter-spacing:.05em}
td{padding:8px 14px;border-bottom:1px solid #f1f3f5;vertical-align:middle}
tr:last-child td{border-bottom:none}
tbody tr:hover td{background:#f8f9fa}
.test-name{max-width:600px;word-break:break-word}
.dur{text-align:right;white-space:nowrap;color:#6c757d;font-variant-numeric:tabular-nums}
.detail-msg{font-family:monospace;font-size:.75rem;color:#721c24;background:#fff5f5;
  padding:6px 10px;border-radius:4px;margin-top:5px;white-space:pre-wrap;
  word-break:break-all;max-height:200px;overflow:auto}
/* coverage */
.pct-cell{display:flex;align-items:center;gap:8px}
.pct-val{width:46px;font-weight:700;text-align:right;flex-shrink:0;
  font-variant-numeric:tabular-nums;font-size:.85rem}
.bar-bg{height:8px;border-radius:4px;background:#e9ecef;overflow:hidden;flex-shrink:0}
.bar-fill{height:100%;border-radius:4px;transition:width .3s}
.pkg-row{background:#f8f9fa;cursor:pointer}
.pkg-row:hover td{background:#eef0f3}
.pkg-name{font-weight:600;font-size:.8rem;color:#495057;
  text-transform:uppercase;letter-spacing:.04em;padding-left:14px!important}
.pkg-arrow{display:inline-block;transition:transform .18s;
  font-size:.7rem;color:#adb5bd;margin-right:6px}
.pkg-arrow.open{transform:rotate(90deg)}
footer{text-align:center;color:#adb5bd;font-size:.75rem;padding:16px 0 32px}
"""


# ─── JavaScript ───────────────────────────────────────────────────────────────

_JS = """
function togglePkg(pkgId) {
  var rows = document.querySelectorAll('#' + CSS.escape(pkgId));
  var arrow = document.getElementById(pkgId + '-arrow');
  var show = rows.length > 0 && rows[0].style.display === 'none';
  rows.forEach(function(r){ r.style.display = show ? '' : 'none'; });
  if (arrow) {
    if (show) arrow.classList.add('open'); else arrow.classList.remove('open');
  }
}
"""


# ─── Main report builder ──────────────────────────────────────────────────────

def generate(args):
    unit_junit = parse_junit(args.unit_junit)
    int_junit  = parse_junit(args.int_junit)
    unit_cov   = parse_cover_func(args.unit_coverage)
    int_cov    = parse_cover_func(args.int_coverage)
    unit_fpct  = read_float(args.unit_func_pct)
    int_fpct   = read_float(args.int_func_pct)

    os_label   = (args.os_label or "").strip()
    build_num  = (args.build_number or "").strip()
    branch     = (args.branch or "").strip()
    now        = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    parts      = [p for p in [
                    os_label,
                    f"Branch: {branch}" if branch else "",
                    f"Build #{build_num}" if build_num else "",
                    now,
                  ] if p]
    subtitle   = "  ·  ".join(parts)

    body = "".join([
        summary_cards_html(unit_junit, unit_cov, unit_fpct, int_junit, int_cov, int_fpct),
        test_table_html(unit_junit, "unit-tests",
                        "Unit Test Results", open_by_default=True),
        test_table_html(int_junit, "int-tests",
                        "Integration Test Results", open_by_default=True),
        coverage_section_html(unit_cov, unit_fpct, "unit-cov",
                              "Unit Test Coverage"),
        coverage_section_html(int_cov, int_fpct, "int-cov",
                              "Integration Test Coverage"),
    ])

    html_out = f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>CB Event Forwarder — Test &amp; Coverage Report</title>
<style>{_CSS}</style>
</head>
<body>
<header>
  <h1>CB Event Forwarder &mdash; Test &amp; Coverage Report</h1>
  <p>{esc(subtitle)}</p>
</header>
<div class="container">
{body}
</div>
<footer>
  Generated by <code>scripts/generate_test_report.py</code> on {esc(now)}
</footer>
<script>{_JS}</script>
</body>
</html>
"""

    out_path = args.output
    os.makedirs(os.path.dirname(os.path.abspath(out_path)), exist_ok=True)
    with open(out_path, "w", encoding="utf-8") as fh:
        fh.write(html_out)
    print(f"Report written to: {out_path}")


def main():
    p = argparse.ArgumentParser(
        description="Generate a consolidated HTML test and coverage report."
    )
    p.add_argument("--unit-junit",
                   default="build/test-results/junit.xml",
                   help="Unit test JUnit XML path")
    p.add_argument("--unit-coverage",
                   default="build/code_coverage/unittest_coverage.txt",
                   help="Unit test 'go tool cover -func' output path")
    p.add_argument("--unit-func-pct",
                   default="build/code_coverage/function_coverage_percent.txt",
                   help="Unit test function coverage percent file")
    p.add_argument("--int-junit",
                   default="build/integration-test-results/junit.xml",
                   help="Integration test JUnit XML path")
    p.add_argument("--int-coverage",
                   default="build/integration-test-results/integration_coverage.txt",
                   help="Integration test 'go tool cover -func' output path")
    p.add_argument("--int-func-pct",
                   default="build/integration-test-results/function_coverage_percent.txt",
                   help="Integration test function coverage percent file")
    p.add_argument("--os-label",      default="",  help="OS label (e.g. EL8, EL9)")
    p.add_argument("--build-number",  default="",  help="Jenkins build number")
    p.add_argument("--branch",        default="",  help="Git branch name")
    p.add_argument("--output", default="build/test-report/report.html",
                   help="Output HTML file path")
    generate(p.parse_args())


if __name__ == "__main__":
    main()
