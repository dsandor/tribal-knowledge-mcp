#!/usr/bin/env python3
"""
enrich-context.py — UserPromptSubmit hook for tribal-knowledge MCP.

Fetches relevant knowledge entries for the current prompt and prints them
as context for Claude. Exits silently on any error to avoid disrupting
Claude Code.
"""

import json
import os
import sys
import urllib.parse
import urllib.request


def main():
    try:
        # Allow the hook to be disabled via env var
        tkm_enabled = os.environ.get("TKM_ENABLED", "true").lower()
        if tkm_enabled in ("false", "0"):
            return

        # Read configuration
        base_url = os.environ.get("TKM_API_URL", "http://localhost:8080").rstrip("/")
        api_key = os.environ.get("TKM_API_KEY", "")

        # Read prompt from stdin (Claude Code sends JSON: {"prompt": "..."})
        try:
            raw = sys.stdin.read()
            data = json.loads(raw)
            prompt = data.get("prompt", "")
        except Exception:
            return

        if not prompt:
            return

        # Build the search URL
        encoded_prompt = urllib.parse.quote(prompt[:500])  # cap query length
        url = f"{base_url}/api/knowledge?search={encoded_prompt}&limit=3"

        # Build the request with optional Bearer auth
        req = urllib.request.Request(url)
        req.add_header("Accept", "application/json")
        if api_key:
            req.add_header("Authorization", f"Bearer {api_key}")

        # Execute with a 2-second timeout
        try:
            with urllib.request.urlopen(req, timeout=2) as resp:
                body = resp.read().decode("utf-8", errors="replace")
                response_data = json.loads(body)
        except Exception:
            # Server not running, auth error, timeout, etc. — exit silently
            return

        # Normalize response: accept list or {"entries": [...]} wrapper
        if isinstance(response_data, list):
            entries = response_data
        elif isinstance(response_data, dict):
            entries = response_data.get("entries", response_data.get("data", []))
        else:
            return

        if not entries:
            return

        # Print the context block to stdout
        lines = ["[Tribal Knowledge Context]", "Relevant knowledge entries for this request:"]
        for entry in entries:
            if not isinstance(entry, dict):
                continue
            title = entry.get("title", entry.get("name", "Untitled"))
            domain = entry.get("domain", entry.get("category", "general"))
            content = entry.get("content", entry.get("body", ""))
            if len(content) > 200:
                content = content[:197] + "..."
            lines.append(f"- {title} (domain: {domain}): {content}")

        print("\n".join(lines))

    except Exception:
        # Catch-all: never let this hook crash Claude Code
        pass


if __name__ == "__main__":
    main()
