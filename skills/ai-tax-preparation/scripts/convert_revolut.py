"""Normalize authoritative Revolut exports without reordering their ledger rows."""
from __future__ import annotations

import argparse
import csv
import json
import sys
from datetime import datetime
from pathlib import Path
from bank_common import FIELDS, money, source_id, transaction_id, write_csv, year


def date(text: str) -> str:
    return datetime.fromisoformat(text.replace("Z", "+00:00")).date().isoformat()


def convert(exports: list[Path], filing_year: str, account: str, states: dict) -> list[dict]:
    year(filing_year)
    if not account.strip() or any(v not in ("include", "exclude") for v in states.values()):
        raise ValueError("invalid account alias or status mapping")
    rows, hashes, intervals, provider_ids = [], set(), {}, set()
    required = {"State", "Product", "Currency", "Amount", "Fee", "Balance",
                "Completed Date", "Started Date", "Type", "Description"}
    for export in exports:
        digest = source_id(export)
        if digest in hashes:
            raise ValueError("duplicate export content")
        hashes.add(digest)
        with export.open(encoding="utf-8-sig", newline="") as handle:
            reader = csv.DictReader(handle)
            if len(reader.fieldnames or []) != len(set(reader.fieldnames or [])):
                raise ValueError("duplicate CSV headers")
            if not required.issubset(reader.fieldnames or []):
                raise ValueError("unsupported Revolut headers")
            records = list(reader)
        if not records:
            raise ValueError("empty Revolut export")
        if any(None in record for record in records):
            raise ValueError("CSV row has extra fields")
        balances, observed, previous_dates, excluded = {}, {}, {}, {}
        for source_row, record in enumerate(records, 2):
            state = record["State"]
            if state not in states:
                raise ValueError("unknown Revolut state; supply --status-map")
            if states[state] == "exclude":
                currency = record.get("Currency") or "currency-unavailable"
                product = record.get("Product") or "product-unavailable"
                count, raw_total, unavailable = excluded.get((product, state, currency), (0, money("0"), 0))
                try:
                    raw_total += money(record["Amount"])
                except (ValueError, KeyError, TypeError):
                    unavailable += 1
                excluded[(product, state, currency)] = (count + 1, raw_total, unavailable)
                continue
            key = (record["Product"], record["Currency"])
            if not all(key):
                raise ValueError("missing product/currency")
            amount, fee, balance = money(record["Amount"]), money(record["Fee"]), money(record["Balance"])
            if fee < 0:
                raise ValueError("negative fee convention requires adaptation")
            net = amount - fee
            booking = date(record["Completed Date"])
            if key in previous_dates and booking < previous_dates[key]:
                raise ValueError("export is not in chronological ledger order; adapt explicitly")
            previous_dates[key] = booking
            if key in balances and balances[key] + net != balance:
                raise ValueError("Revolut balance chain failed")
            balances[key] = balance
            observed.setdefault(key, []).append(booking)
            provider_id = record.get("ID", "") or record.get("Transaction ID", "")
            alias = f"{account}:{key[0]}:{key[1]}"
            identity = f"provider:{alias}:{provider_id}" if provider_id else transaction_id(digest, source_row, alias)
            if provider_id:
                if identity in provider_ids:
                    raise ValueError("duplicate provider transaction ID")
                provider_ids.add(identity)
            if booking[:4] == filing_year:
                rows.append(dict(zip(FIELDS, [export.name, source_row, identity, booking,
                    date(record["Started Date"]), record["Type"] + " " + record["Description"],
                    "", f"{net:.2f}", key[1], f"{balance:.2f}", alias,
                    f"raw_amount={amount:.2f}; fee={fee:.2f}"])))
        if not observed:
            raise ValueError("export contains no included ledger rows")
        for (product, state, currency), (count, raw_total, unavailable) in sorted(excluded.items()):
            print(f"source {export.name}: excluded {product}/{state}/{currency}: {count} rows; "
                  f"available raw amount subtotal {raw_total:.2f}; absent/unverified amounts {unavailable}; "
                  "raw values are not cash effects")
        for key, dates in observed.items():
            interval = (min(dates), max(dates))
            for start, end in intervals.get(key, []):
                if interval[0] <= end and start <= interval[1]:
                    raise ValueError("overlapping ledger exports; select one authoritative export")
            intervals.setdefault(key, []).append(interval)
            print(f"source {export.name}: {key[0]}/{key[1]} observed {interval[0]}..{interval[1]}; "
                  "chain reconciled; first opening inferred, cross-file continuity unavailable; annual coverage unverified")
    return rows


def main() -> None:
    cli = argparse.ArgumentParser(description=__doc__)
    cli.add_argument("out_csv", type=Path)
    cli.add_argument("exports", type=Path, nargs="+")
    cli.add_argument("--year", required=True)
    cli.add_argument("--account", required=True)
    cli.add_argument("--status-map", type=Path, help='JSON state -> "include" or "exclude"')
    args = cli.parse_args()
    try:
        if args.out_csv.exists():
            raise FileExistsError("refusing to overwrite output")
        states = {"COMPLETED": "include", "REVERTED": "exclude"}
        if args.status_map:
            states.update(json.loads(args.status_map.read_text()))
        rows = convert(args.exports, args.year, args.account, states)
        write_csv(args.out_csv, rows)
    except (ValueError, OSError, RuntimeError, KeyError) as error:
        sys.exit(str(error))


if __name__ == "__main__":
    main()
