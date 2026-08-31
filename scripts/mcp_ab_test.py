#!/usr/bin/env python3
"""
Automated A/B token/cost comparison: the same task run through Claude Code's
non-interactive mode (`claude -p ... --output-format json`), once with the
tokensaver MCP connector available and once without (forcing a script/manual
approach) — using --strict-mcp-config so no other locally-registered MCP server
can skew the numbers either way.

This automates the manual "run it twice, check /cost" comparison described in
README.md and TESTING.md.

SAFETY: baseline runs grant the `Bash` tool broadly (and, for the file-fallback
case, MCP runs grant `Bash(curl *)`) via --allowedTools, so Claude can act
non-interactively without a human approving each command. Run this against a
disposable/sandboxed tokensaver instance and in a directory you're fine with
Claude writing/running scripts in — do not point --tokensaver-url at a
production deployment or run this from a directory with sensitive files.

Usage:
    python3 scripts/mcp_ab_test.py
    python3 scripts/mcp_ab_test.py --runs 5 --sample-pdf /path/to/file.pdf
    python3 scripts/mcp_ab_test.py --tokensaver-url http://localhost:8081 --claude-bin ~/.local/bin/claude
"""

import argparse
import json
import subprocess
import sys
import time
import urllib.request
from datetime import datetime
from pathlib import Path

TEST_CASES = [
    {
        "id": "hash_text",
        "prompt": 'Hash the string "hello world" with sha256.',
        "mcp_allowed": "mcp__tokensaver__hash_text",
        "baseline_allowed": "Bash",
        "baseline_disallowed": "",
        "requires_pdf": False,
    },
    {
        "id": "convert_data",
        "prompt": 'Convert this JSON to YAML: {"name": "Milan", "role": "developer"}',
        "mcp_allowed": "mcp__tokensaver__convert_data",
        "baseline_allowed": "Bash",
        "baseline_disallowed": "",
        "requires_pdf": False,
    },
    {
        "id": "diff_data",
        "prompt": 'Diff these two JSON objects: {"a":1,"b":2} and {"a":1,"b":3,"c":4}',
        "mcp_allowed": "mcp__tokensaver__diff_data",
        "baseline_allowed": "Bash",
        "baseline_disallowed": "",
        "requires_pdf": False,
    },
    {
        "id": "base64",
        "prompt": 'Base64 encode the string "tokensaver rocks"',
        "mcp_allowed": "mcp__tokensaver__base64",
        "baseline_allowed": "Bash",
        "baseline_disallowed": "",
        "requires_pdf": False,
    },
    {
        "id": "web_extract",
        "prompt": "Fetch https://example.com and summarize the page in one sentence.",
        "mcp_allowed": "mcp__tokensaver__web_extract",
        "baseline_allowed": "Bash",
        # Disallow Claude Code's own built-in web tool so the baseline reflects
        # "no tokensaver" rather than "has a different built-in shortcut" —
        # this is the scenario tokensaver targets (agents/SDKs without one).
        "baseline_disallowed": "WebFetch WebSearch",
        "requires_pdf": False,
    },
    {
        "id": "evaluate_formula",
        "prompt": "Evaluate =SUM(A1:A3)*2 where A1=2, A2=3, A3=4",
        "mcp_allowed": "mcp__tokensaver__evaluate_formula",
        "baseline_allowed": "Bash",
        "baseline_disallowed": "",
        "requires_pdf": False,
    },
    {
        "id": "pdf_extract_fallback",
        "prompt": "Extract the text from {pdf} and show me the first 100 characters.",
        "mcp_allowed": "Bash(curl *)",
        "baseline_allowed": "Bash",
        # Block Claude Code's own native PDF reading for both variants so this
        # measures script-avoidance, not "does the harness already read PDFs."
        "baseline_disallowed": "Read",
        "mcp_disallowed": "Read",
        "requires_pdf": True,
    },
]


def check_server(base_url):
    try:
        with urllib.request.urlopen(f"{base_url}/api/mcp-tools", timeout=5) as resp:
            return resp.status == 200
    except Exception as e:
        print(f"ERROR: tokensaver isn't reachable at {base_url}: {e}", file=sys.stderr)
        return False


def write_mcp_config(path, base_url):
    config = {"mcpServers": {"tokensaver": {"type": "http", "url": f"{base_url}/mcp"}}}
    path.write_text(json.dumps(config))


def run_claude(claude_bin, prompt, mcp_config, allowed, disallowed):
    cmd = [claude_bin, "-p", prompt, "--strict-mcp-config", "--output-format", "json"]
    if mcp_config:
        cmd += ["--mcp-config", str(mcp_config)]
    if allowed:
        cmd += ["--allowedTools", allowed]
    if disallowed:
        cmd += ["--disallowedTools", disallowed]
    proc = subprocess.run(cmd, capture_output=True, text=True, timeout=300)
    if proc.returncode != 0:
        return {"error": f"exit {proc.returncode}: {proc.stderr.strip()[:500]}"}
    try:
        return json.loads(proc.stdout)
    except json.JSONDecodeError as e:
        return {"error": f"bad JSON output: {e}: {proc.stdout[:500]}"}


def summarize(result):
    if "error" in result:
        return None
    usage = result.get("usage", {})
    total_tokens = (
        usage.get("input_tokens", 0)
        + usage.get("cache_creation_input_tokens", 0)
        + usage.get("cache_read_input_tokens", 0)
        + usage.get("output_tokens", 0)
    )
    return {
        "cost_usd": result.get("total_cost_usd", 0.0),
        "total_tokens": total_tokens,
        "output_tokens": usage.get("output_tokens", 0),
        "is_error": result.get("is_error", False),
        "result_text": (result.get("result") or "")[:200],
    }


def average(dicts, key):
    vals = [d[key] for d in dicts if d]
    return sum(vals) / len(vals) if vals else 0.0


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--tokensaver-url", default="http://localhost:8080")
    parser.add_argument("--claude-bin", default="claude")
    parser.add_argument("--runs", type=int, default=3, help="repetitions per variant per case")
    parser.add_argument("--sample-pdf", default=None, help="path to a PDF for the file-fallback case")
    parser.add_argument("--output-dir", default=None)
    parser.add_argument("--only", default=None, help="comma-separated case ids to run (default: all)")
    args = parser.parse_args()

    if not check_server(args.tokensaver_url):
        sys.exit(1)

    out_dir = Path(args.output_dir or f".ab-results/{datetime.now():%Y%m%d-%H%M%S}")
    out_dir.mkdir(parents=True, exist_ok=True)
    mcp_config = out_dir / "mcp-config.json"
    write_mcp_config(mcp_config, args.tokensaver_url)

    only = set(args.only.split(",")) if args.only else None
    cases = [c for c in TEST_CASES if not only or c["id"] in only]

    print(f"Results will be saved to {out_dir}/\n")
    rows = []

    for case in cases:
        if case["requires_pdf"] and not args.sample_pdf:
            print(f"SKIP {case['id']}: needs --sample-pdf <path>")
            continue
        prompt = case["prompt"].format(pdf=args.sample_pdf) if case["requires_pdf"] else case["prompt"]

        case_dir = out_dir / case["id"]
        case_dir.mkdir(exist_ok=True)

        for variant, mcp_cfg, allowed, disallowed in [
            ("with_mcp", mcp_config, case["mcp_allowed"], case.get("mcp_disallowed", "")),
            ("baseline", None, case["baseline_allowed"], case.get("baseline_disallowed", "")),
        ]:
            variant_dir = case_dir / variant
            variant_dir.mkdir(exist_ok=True)
            summaries = []
            print(f"[{case['id']}] {variant}: ", end="", flush=True)
            for i in range(1, args.runs + 1):
                result = run_claude(args.claude_bin, prompt, mcp_cfg, allowed, disallowed)
                (variant_dir / f"run-{i}.json").write_text(json.dumps(result, indent=2))
                s = summarize(result)
                summaries.append(s)
                print("." if s and not s["is_error"] else "x", end="", flush=True)
                time.sleep(1)
            print()
            valid = [s for s in summaries if s]
            rows.append({
                "case": case["id"],
                "variant": variant,
                "runs": len(valid),
                "avg_cost_usd": average(valid, "cost_usd"),
                "avg_total_tokens": average(valid, "total_tokens"),
                "errors": len(summaries) - len(valid),
            })

    print("\n" + "=" * 100)
    print(f"{'case':<24}{'variant':<12}{'runs':<6}{'avg cost ($)':<16}{'avg total tokens':<18}{'errors'}")
    print("-" * 100)
    for r in rows:
        print(f"{r['case']:<24}{r['variant']:<12}{r['runs']:<6}{r['avg_cost_usd']:<16.5f}{r['avg_total_tokens']:<18.0f}{r['errors']}")

    print("\nSavings (with_mcp vs baseline):")
    by_case = {}
    for r in rows:
        by_case.setdefault(r["case"], {})[r["variant"]] = r
    for case_id, variants in by_case.items():
        if "with_mcp" in variants and "baseline" in variants:
            b, m = variants["baseline"], variants["with_mcp"]
            if b["avg_cost_usd"] > 0:
                pct = (b["avg_cost_usd"] - m["avg_cost_usd"]) / b["avg_cost_usd"] * 100
                print(f"  {case_id}: {pct:+.1f}% cost change with tokensaver "
                      f"(${m['avg_cost_usd']:.5f} vs ${b['avg_cost_usd']:.5f})")

    print(f"\nRaw per-run JSON saved under {out_dir}/<case>/<variant>/run-N.json")


if __name__ == "__main__":
    main()
