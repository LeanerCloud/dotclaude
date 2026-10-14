"""Purpose tags are proposals, not FX conversion or tax eligibility decisions."""
from __future__ import annotations

import argparse
import csv
import json
import re
import sys
from pathlib import Path
from bank_common import money, write_csv

METADATA = ["category", "vendor", "classification_status", "rule_ids", "classification_evidence"]


def classify(rows: list[dict], rules: list[dict], overrides: dict) -> list[dict]:
    identities = [row["transaction_id"] for row in rows]
    if len(set(identities)) != len(identities) or any(not identity for identity in identities):
        raise ValueError("missing or duplicate transaction ID")
    if set(overrides) - set(identities):
        raise ValueError("unmatched explicit override IDs; map changed sources before rerunning")
    compiled, rule_ids = [], set()
    for rule in rules:
        if (rule["field"] not in ("counterparty", "purpose", "combined")
                or rule["sign"] not in ("any", "debit", "credit")
                or not rule["id"] or rule["id"] in rule_ids
                or not rule["category"] or not isinstance(rule["vendor"], str)):
            raise ValueError("invalid rule")
        rule_ids.add(rule["id"])
        compiled.append((rule, re.compile(rule["regex"], re.I)))
    for row in rows:
        amount = money(row["amount"])
        if not isinstance(row.get("currency"), str) or not row["currency"].strip():
            raise ValueError("transaction currency must not be empty")
        confirmed = row.get("classification_status") == "confirmed_override"
        old = {"category": row.get("category", ""), "vendor": row.get("vendor", ""),
               "evidence": row.get("classification_evidence", "")}
        override = overrides.get(row["transaction_id"])
        if row["transaction_id"] in overrides and (not isinstance(override, dict)
                or set(override) != {"category", "vendor", "evidence"}):
            raise ValueError("malformed explicit override")
        if confirmed and (not old["category"] or not old["evidence"].strip()):
            raise ValueError("confirmed override lacks category/evidence")
        if confirmed and override and old != override:
            raise ValueError("conflicting confirmed override; resolve explicitly")
        override = override or (old if confirmed else None)
        if override:
            if (set(override) != {"category", "vendor", "evidence"}
                    or not all(isinstance(v, str) for v in override.values())
                    or not override["category"].strip() or not override["evidence"].strip()):
                raise ValueError("override requires category, vendor, and nonempty evidence")
            row.update(category=override["category"], vendor=override["vendor"],
                       classification_status="confirmed_override", rule_ids="",
                       classification_evidence=override["evidence"])
            continue
        matches = []
        for rule, pattern in compiled:
            text = (row["counterparty"] + " || " + row["purpose"]
                    if rule["field"] == "combined" else row[rule["field"]])
            if (rule["sign"] == "debit" and amount >= 0) or (rule["sign"] == "credit" and amount <= 0):
                continue
            if pattern.search(text):
                matches.append(rule)
        choices = {(rule["category"], rule["vendor"]) for rule in matches}
        category, vendor = ((matches[0]["category"], matches[0]["vendor"]) if len(choices) == 1
                            else ("Review:Ambiguous" if choices else "Review:Unidentified", ""))
        row.update(category=category, vendor=vendor,
                   classification_status="rule_proposal" if len(choices) == 1 and not category.startswith("Review:") else "review",
                   rule_ids=";".join(rule["id"] for rule in matches), classification_evidence="")
    return rows


def main() -> None:
    cli = argparse.ArgumentParser(description=__doc__)
    cli.add_argument("input_csv", type=Path)
    cli.add_argument("out_csv", type=Path)
    cli.add_argument("--rules", required=True, type=Path)
    cli.add_argument("--overrides", required=True, type=Path)
    args = cli.parse_args()
    try:
        if args.out_csv.exists():
            raise FileExistsError("refusing to overwrite output")
        with args.input_csv.open(encoding="utf-8", newline="") as handle:
            reader = csv.DictReader(handle)
            fields = reader.fieldnames
            if len(fields or []) != len(set(fields or [])):
                raise ValueError("duplicate CSV headers")
            rows = list(reader)
            if any(None in row for row in rows):
                raise ValueError("CSV row has extra fields")
        if not fields or not rows:
            raise ValueError("empty transaction input")
        rows = classify(rows, json.loads(args.rules.read_text()), json.loads(args.overrides.read_text()))
        write_csv(args.out_csv, rows, [field for field in fields if field not in METADATA] + METADATA)
        totals = {}
        for row in rows:
            if row["classification_status"] == "review":
                count, amount = totals.get(row["currency"], (0, money("0")))
                totals[row["currency"]] = (count + 1, amount + money(row["amount"]))
        for currency, (count, amount) in sorted(totals.items()):
            print(f"review {currency}: {count} rows, signed raw total {amount:.2f}")
    except (ValueError, OSError, RuntimeError, KeyError, re.error) as error:
        sys.exit(str(error))


if __name__ == "__main__":
    main()
