#!/usr/bin/env python3
"""Answer Claude's usage-limit prompt with "Wait here, then continue automatically"
in every herdr agent that is showing it.

Usage: herdr-wait-for-limit-reset.py [--dry-run]

Only touches a pane whose screen shows the limit prompt with the cursor on
option 1, so it never presses keys into an approval dialog or a busy agent.
Never picks "Switch to usage credits".
"""
import json
import subprocess
import sys
import time

PROMPT = "What do you want to do?"
WAIT_OPTION = "2. Wait here, then continue automatically"
CURSOR_ON_FIRST = "❯ 1. Stop and wait for limit to reset"


def herdr(*args: str) -> str:
    return subprocess.run(
        ["herdr", *args], check=True, capture_output=True, text=True
    ).stdout


def screen(name: str) -> str:
    return herdr("agent", "read", name, "--source", "visible")


def main() -> int:
    dry_run = "--dry-run" in sys.argv[1:]
    agents = json.loads(herdr("agent", "list"))["result"]["agents"]
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
        text = screen(name)
        if PROMPT not in text or WAIT_OPTION not in text:
            print(f"skip  {name}: blocked on something other than the usage-limit prompt")
            continue
        if CURSOR_ON_FIRST not in text:
            print(f"skip  {name}: cursor is not on option 1, not guessing")
            continue
        if dry_run:
            print(f"would {name}: select option 2")
            continue
        herdr("agent", "send-keys", name, "down", "enter")
        time.sleep(2)
        if PROMPT in screen(name):
            print(f"FAIL  {name}: prompt still showing")
            failed += 1
        else:
            print(f"ok    {name}: will continue automatically at reset")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
