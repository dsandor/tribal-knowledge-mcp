#!/usr/bin/env python3
"""
Seed the tribal knowledge MCP server with realistic example entries.

Usage:
    python scripts/seed.py                        # defaults to http://localhost:8080
    python scripts/seed.py --base-url http://...  # custom server
    python scripts/seed.py --api-key sk-...       # provide auth header
    python scripts/seed.py --dry-run              # print payloads without posting

Requires: Python 3.9+ and the 'requests' package (pip install requests).
"""

import argparse
import json
import sys
import time
from typing import Optional

try:
    import requests
except ImportError:
    print("ERROR: 'requests' package not found. Run: pip install requests", file=sys.stderr)
    sys.exit(1)

# ── Seed data ─────────────────────────────────────────────────────────────────

SEED_ENTRIES = [
    # Domain: financial-analysis
    {
        "title": "Earnings call transcript prompt template",
        "content": (
            "When analysing an earnings call transcript, structure your prompt as follows:\n"
            "1. CONTEXT: Provide the company ticker, fiscal quarter, and analyst consensus EPS.\n"
            "2. TASK: Ask the LLM to extract (a) management tone, (b) forward guidance deltas vs prior quarter, "
            "(c) top three risk factors mentioned.\n"
            "3. OUTPUT FORMAT: Request a markdown table with columns: Topic | Signal | Sentiment | Confidence.\n"
            "Teams using this template report 40% reduction in missed signals compared to free-form prompts."
        ),
        "type": "prompt_template",
        "domain": "financial-analysis",
        "tags": ["earnings", "transcript", "prompt-engineering"],
    },
    {
        "title": "Sector rotation signal checklist",
        "content": (
            "Before concluding a sector rotation recommendation, verify:\n"
            "- Relative strength index (RSI) cross-sector comparison over 90-day window\n"
            "- Fed funds rate direction vs historical sector beta\n"
            "- Yield curve slope (2-10 spread) compared to prior rotation cycles\n"
            "- Commodity index correlation for energy/materials sectors\n"
            "Prompt the LLM with: 'Given [data], identify which sectors are early/mid/late cycle and assign confidence 1-5.'"
        ),
        "type": "checklist",
        "domain": "financial-analysis",
        "tags": ["sector-rotation", "macro", "checklist"],
    },
    {
        "title": "Avoid recency bias in LLM stock reports",
        "content": (
            "LLMs weight recent training data heavily. When generating a stock report:\n"
            "- Explicitly provide 5-year CAGR alongside TTM growth to anchor long-term context.\n"
            "- Ask the model to compare current P/E to the 10-year median, not just the 52-week range.\n"
            "- Anti-pattern: asking 'Is this stock a good buy?' without date context — model anchors on last known price.\n"
            "- Best practice: include 'As of [date], with the stock at [price]...' in every prompt."
        ),
        "type": "best_practice",
        "domain": "financial-analysis",
        "tags": ["bias", "stock-analysis", "prompt-engineering"],
    },

    # Domain: software-engineering
    {
        "title": "Code review prompt for security vulnerabilities",
        "content": (
            "Use this prompt structure when asking an LLM to review code for security issues:\n\n"
            "```\n"
            "Review the following [language] code for security vulnerabilities.\n"
            "Focus on: SQL injection, XSS, insecure deserialization, hardcoded secrets, "
            "improper error handling that leaks stack traces.\n"
            "For each finding: describe the vulnerability, assign CVSS severity (Low/Med/High/Critical), "
            "and provide a remediation code snippet.\n"
            "```\n\n"
            "Attach the code block after the prompt. Teams report this yields 3x more actionable findings "
            "than 'look for bugs in this code'."
        ),
        "type": "prompt_template",
        "domain": "software-engineering",
        "tags": ["security", "code-review", "prompt-engineering"],
    },
    {
        "title": "Architecture decision record (ADR) generation prompt",
        "content": (
            "To generate a high-quality ADR using an LLM:\n"
            "1. State the decision context: system name, scale, constraints, and the options considered.\n"
            "2. Ask the model to produce: Title, Status, Context, Decision, Consequences (positive and negative), "
            "and Alternatives Considered sections.\n"
            "3. Include a 'Risks' section by appending: 'List the top three risks and a mitigation for each.'\n"
            "ADRs generated this way consistently pass architecture review without major revisions."
        ),
        "type": "prompt_template",
        "domain": "software-engineering",
        "tags": ["architecture", "adr", "documentation"],
    },
    {
        "title": "Debugging prompts: give the LLM full context",
        "content": (
            "When asking an LLM to debug a failing test or runtime error, always include:\n"
            "- The full error message and stack trace (not a summary)\n"
            "- The relevant code section (not the entire file)\n"
            "- What you have already tried\n"
            "- The Go/Python/Rust version and OS\n\n"
            "Anti-pattern: 'This function doesn't work, fix it.' - LLM will produce a generic response.\n"
            "Best practice: 'This function fails with [error] on line [N] when input is [X]. "
            "I tried [Y] and [Z]. Here is the code: [snippet].'"
        ),
        "type": "best_practice",
        "domain": "software-engineering",
        "tags": ["debugging", "context", "prompt-engineering"],
    },

    # Domain: data-science
    {
        "title": "EDA summary prompt for tabular datasets",
        "content": (
            "Paste the output of `df.describe()` and `df.info()` into this prompt:\n\n"
            "```\n"
            "You are a senior data scientist. Given the following dataset summary:\n"
            "[INSERT df.describe() output]\n"
            "[INSERT df.info() output]\n\n"
            "Identify: (1) columns with high missing-value rates (>20%), "
            "(2) potential outlier columns based on mean/std ratio, "
            "(3) columns that are likely categorical despite numeric dtype, "
            "(4) recommended feature engineering steps.\n"
            "Output a markdown report with a findings table and recommendations list.\n"
            "```"
        ),
        "type": "prompt_template",
        "domain": "data-science",
        "tags": ["eda", "pandas", "feature-engineering"],
    },
    {
        "title": "Model evaluation prompt: avoid cherry-picked metrics",
        "content": (
            "When asking an LLM to evaluate a model's performance, require a complete picture:\n"
            "- For classification: accuracy, precision, recall, F1, AUC-ROC, and confusion matrix interpretation.\n"
            "- For regression: MAE, RMSE, R-squared, and residual plot description.\n"
            "- Always ask: 'Is the dataset class-balanced? If not, which metric should be prioritized and why?'\n\n"
            "Anti-pattern: reporting only accuracy on an imbalanced dataset - LLM will validate a useless model.\n"
            "Pair this with: 'What would a naive baseline (majority class / mean prediction) score on these same metrics?'"
        ),
        "type": "best_practice",
        "domain": "data-science",
        "tags": ["model-evaluation", "metrics", "imbalance"],
    },
    {
        "title": "Reproducibility checklist for ML experiments",
        "content": (
            "Before sharing an ML experiment result, verify:\n"
            "- Random seeds set for numpy, torch/tensorflow, and the train/test split\n"
            "- Library versions pinned in requirements.txt or environment.yaml\n"
            "- Dataset version and preprocessing steps documented\n"
            "- Hyperparameter search log saved (not just the best params)\n\n"
            "Prompt template for LLM-assisted review:\n"
            "'Review this ML experiment setup for reproducibility issues. "
            "Here is my code: [snippet] and my results: [metrics]. "
            "Flag anything that would prevent a colleague from reproducing these results exactly.'"
        ),
        "type": "checklist",
        "domain": "data-science",
        "tags": ["reproducibility", "mlops", "best-practices"],
    },
]


# ── Client ────────────────────────────────────────────────────────────────────

def post_entry(base_url, headers, entry, dry_run):
    url = f"{base_url.rstrip('/')}/api/knowledge"
    if dry_run:
        print(f"[DRY RUN] POST {url}")
        print(json.dumps(entry, indent=2))
        return None
    resp = requests.post(url, json=entry, headers=headers, timeout=15)
    if resp.status_code == 409:
        print(f"  SKIP (already exists): {entry['title']}")
        return None
    resp.raise_for_status()
    data = resp.json()
    return data.get("id")


def main():
    parser = argparse.ArgumentParser(description="Seed the tribal knowledge MCP server")
    parser.add_argument("--base-url", default="http://localhost:8080", help="Server base URL")
    parser.add_argument("--api-key", default="", help="API key for Authorization header")
    parser.add_argument("--dry-run", action="store_true", help="Print payloads without POSTing")
    parser.add_argument("--delay", type=float, default=0.2, help="Seconds between requests (default 0.2)")
    args = parser.parse_args()

    headers = {"Content-Type": "application/json"}
    if args.api_key:
        headers["Authorization"] = f"Bearer {args.api_key}"

    print(f"Seeding {len(SEED_ENTRIES)} entries to {args.base_url}")
    if args.dry_run:
        print("(dry-run mode - no requests will be sent)\n")

    created = 0
    skipped = 0
    errors = 0

    for entry in SEED_ENTRIES:
        print(f"  POST [{entry['domain']}] {entry['title'][:60]}...")
        try:
            eid = post_entry(args.base_url, headers, entry, args.dry_run)
            if eid:
                print(f"    -> created: {eid}")
                created += 1
            elif not args.dry_run:
                skipped += 1
        except requests.HTTPError as e:
            print(f"    ERROR: {e.response.status_code} {e.response.text[:120]}", file=sys.stderr)
            errors += 1
        except Exception as e:
            print(f"    ERROR: {e}", file=sys.stderr)
            errors += 1
        if not args.dry_run:
            time.sleep(args.delay)

    print(f"\nDone. created={created} skipped={skipped} errors={errors}")
    if errors:
        sys.exit(1)


if __name__ == "__main__":
    main()
