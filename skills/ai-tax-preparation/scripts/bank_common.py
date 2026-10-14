"""Shared controls for the editable statement examples."""
from __future__ import annotations

import csv
import hashlib
import re
import subprocess
from datetime import datetime
from decimal import Decimal, InvalidOperation
from pathlib import Path

FIELDS = ["source_file", "source_row", "transaction_id", "booking_date", "value_date",
          "counterparty", "purpose", "amount", "currency", "balance_after", "account",
          "settlement_detail"]


def money(text: str, german: bool = False) -> Decimal:
    try:
        value = Decimal(text.replace(".", "").replace(",", ".") if german else text)
        if not value.is_finite() or value != value.quantize(Decimal("0.01")):
            raise ValueError("amount must be finite with at most two decimal places")
    except InvalidOperation as error:
        raise ValueError("invalid decimal amount") from error
    return value


def iso(text: str) -> str:
    return datetime.strptime(text, "%d.%m.%Y").date().isoformat()


def year(text: str) -> str:
    if not re.fullmatch(r"[1-9]\d{3}", text):
        raise ValueError("year must be four digits")
    return text


def pdf_text(path: Path) -> list[str]:
    return subprocess.run(["pdftotext", "-layout", str(path), "-"], check=True,
                          capture_output=True, text=True).stdout.splitlines()


def source_id(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def transaction_id(digest: str, row: int, account: str) -> str:
    return hashlib.sha256(f"{digest}:{row}:{account}".encode()).hexdigest()


def write_csv(path: Path, rows: list[dict], fields: list[str] = FIELDS) -> None:
    handle = path.open("x", encoding="utf-8", newline="")
    try:
        with handle:
            writer = csv.DictWriter(handle, fieldnames=fields)
            writer.writeheader()
            writer.writerows(rows)
    except Exception as error:
        raise RuntimeError("write failed; destination may contain an incomplete artifact") from error


def run_pdf(args, parser) -> None:
    year(args.year)
    if not args.account.strip():
        raise ValueError("account alias must not be empty")
    if args.out_csv.exists():
        raise FileExistsError("refusing to overwrite output")
    sources = sorted(args.pdf_dir.glob("*.pdf"))
    if not sources:
        raise ValueError("no PDF sources found")
    rows, hashes = [], set()
    previous_close = None
    for path in sources:
        digest = source_id(path)
        if digest in hashes:
            raise ValueError("duplicate source content")
        hashes.add(digest)
        opening, closing, entries = parser(pdf_text(path))
        continuity = "unavailable:first-source" if previous_close is None else (
            "balance-match-only" if opening == previous_close else "gap-or-order-unverified")
        previous_close = closing
        dates = [iso(e["bd"]) for e in entries]
        print(f"source {path.name}: reconciled; observed {min(dates) if dates else 'none'}"
              f"..{max(dates) if dates else 'none'}; continuity={continuity}; annual coverage unverified")
        balance = opening
        for entry in entries:
            balance += entry["amt"]
            if iso(entry["bd"])[:4] != args.year:
                continue
            rows.append(dict(zip(FIELDS, [path.name, entry["row"],
                transaction_id(digest, entry["row"], args.account), iso(entry["bd"]),
                iso(entry["vd"]) if entry["vd"] else "", entry["cp"],
                " ".join(entry["pur"]), f"{entry['amt']:.2f}", "EUR",
                f"{balance:.2f}", args.account, ""])))
    write_csv(args.out_csv, rows)


def pdf_cli(parser) -> None:
    import argparse
    import sys
    cli = argparse.ArgumentParser(description="Reconcile complete PDFs, then filter bookings by year.")
    cli.add_argument("pdf_dir", type=Path)
    cli.add_argument("out_csv", type=Path)
    cli.add_argument("--year", required=True)
    cli.add_argument("--account", required=True)
    try:
        run_pdf(cli.parse_args(), parser)
    except (ValueError, OSError, subprocess.CalledProcessError, RuntimeError) as error:
        sys.exit(str(error))
