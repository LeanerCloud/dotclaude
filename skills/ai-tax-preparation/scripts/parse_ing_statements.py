"""German ING layout example; requires pdftotext and an explicit account alias."""
from __future__ import annotations

import re
from decimal import Decimal
from bank_common import money, pdf_cli

DATE = r"\d\d\.\d\d\.\d{4}"
ENTRY = re.compile(rf"^({DATE})\s+(.*?)\s{{2,}}(-?[\d.]+,\d\d)\d?$")
VALUTA = re.compile(rf"^({DATE})\s*(.*)$")


def parse_statement(lines: list[str]) -> tuple[Decimal, Decimal, list[dict]]:
    head = "\n".join(lines[:40])
    old = re.search(r"Alter Saldo\s+(-?[\d.]+,\d\d)", head)
    new = re.search(r"Neuer Saldo\s+(-?[\d.]+,\d\d)", head)
    if not old or not new:
        raise ValueError("ING opening/closing balance missing")
    opening, closing = money(old[1], True), money(new[1], True)
    entries, in_table, saw_table = [], False, False
    for row, raw in enumerate(lines, 1):
        text = raw.strip()
        if text == "Valuta":
            in_table, saw_table = True, True
            continue
        if text.startswith("ING-DiBa AG ·"):
            in_table = False
        if not in_table or not text or re.match(r"^\d\d[A-Z]+\d+_T$", text):
            continue
        match = ENTRY.match(text)
        if match:
            if "%" in match[2]:
                in_table = False
                continue
            if entries and not entries[-1]["vd"]:
                raise ValueError("ING booking lacks value date")
            entries.append(dict(row=row, bd=match[1], cp=match[2],
                                amt=money(match[3], True), pur=[], vd=""))
            continue
        if VALUTA.match(text) and (not entries or entries[-1]["vd"]):
            raise ValueError("ING unparsed booking-like line; adapt observed layout")
        if entries:
            current = entries[-1]
            value = VALUTA.match(text)
            if not current["vd"] and value:
                current["vd"] = value[1]
                if value[2]:
                    current["pur"].append(value[2])
            else:
                current["pur"].append(text)
    if not saw_table:
        raise ValueError("ING transaction table marker missing")
    if not entries and not any(line.strip() == "Keine Umsätze" for line in lines):
        raise ValueError("ING empty extraction lacks explicit no-activity evidence")
    if any(not e["vd"] for e in entries):
        raise ValueError("ING booking lacks value date")
    if opening + sum((e["amt"] for e in entries), Decimal(0)) != closing:
        raise ValueError("ING balance reconciliation failed")
    return opening, closing, entries


if __name__ == "__main__":
    pdf_cli(parse_statement)
