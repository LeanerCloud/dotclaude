#!/usr/bin/env python3
"""Answer Claude's usage-limit prompt with "Wait here, then continue automatically"
in every herdr agent that is showing it.

Usage: herdr-wait-for-limit-reset.py [--dry-run]

Only touches a pane whose screen shows the limit prompt with the cursor on
option 1, so it never presses keys into an approval dialog or a busy agent.
Never picks "Switch to usage credits".
"""
import argparse
import json
import subprocess
import sys
import time

PROMPT = "What do you want to do?"
WAIT_OPTION = "2. Wait here, then continue automatically"
CURSOR_ON_FIRST = "❯ 1. Stop and wait for limit to reset"
HERDR_TIMEOUT_S = 15


def herdr(*args: str) -> str:
    return subprocess.run(
        ["herdr", *args], check=True, capture_output=True, text=True, timeout=HERDR_TIMEOUT_S
    ).stdout


def screen(name: str) -> str:
    return herdr("agent", "read", name, "--source", "visible")


def limit_prompt_on_first(text: str) -> bool:
    return PROMPT in text and WAIT_OPTION in text and CURSOR_ON_FIRST in text


def handle(name: str, dry_run: bool) -> None:
    text = screen(name)
    if PROMPT not in text or WAIT_OPTION not in text:
        print(f"skip  {name}: blocked on something other than the usage-limit prompt")
        return
    if CURSOR_ON_FIRST not in text:
        print(f"skip  {name}: cursor is not on option 1, not guessing")
        return
    if dry_run:
        print(f"would {name}: select option 2")
        return
    if not limit_prompt_on_first(screen(name)):
        print(f"skip  {name}: prompt changed before sending keys")
        return
    herdr("agent", "send-keys", name, "down", "enter")
    time.sleep(2)
    if PROMPT in screen(name):
        raise RuntimeError("prompt still showing")
    print(f"ok    {name}: will continue automatically at reset")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--dry-run", action="store_true")
    dry_run = parser.parse_args().dry_run
    try:
        agents = json.loads(herdr("agent", "list"))["result"]["agents"]
    except (subprocess.TimeoutExpired, subprocess.CalledProcessError, FileNotFoundError) as e:
        print(f"herdr-wait-for-limit-reset: herdr agent list failed: {e}", file=sys.stderr)
        return 1
    blocked = [
        a["name"]
        for a in agents
        if a.get("agent") == "claude" and a.get("agent_status") == "blocked" and a.get("name")
    ]
    if not blocked:
        print("no blocked claude agents")
        return 0

    failed = 0
    for name in blocked:
        try:
            handle(name, dry_run)
        except (subprocess.TimeoutExpired, subprocess.CalledProcessError, RuntimeError) as e:
            detail = (getattr(e, "stderr", None) or "").strip().splitlines()
            print(f"FAIL  {name}: {e}{': ' + detail[-1] if detail else ''}", file=sys.stderr)
            failed += 1
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
