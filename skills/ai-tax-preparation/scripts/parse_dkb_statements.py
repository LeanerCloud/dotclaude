"""German DKB layout example; validates balances and bank cash totals/counts."""
from __future__ import annotations

import re
from decimal import Decimal
from bank_common import money, pdf_cli

DATE = r"\d\d\.\d\d\.\d{4}"
ENTRY = re.compile(rf"^\s+({DATE})\s+(\S.*?)\s{{2,}}(-?[\d.]+,\d\d)\s*$")
OPENING = re.compile(rf"Kontostand am ({DATE}), Auszug Nr\.\s*\d+\s+(-?[\d.]+,\d\d)")
CLOSING = re.compile(rf"Kontostand am ({DATE}) um \S+ Uhr\s+(-?[\d.]+,\d\d)")
WERT = re.compile(rf"Wert:\s*({DATE})")
BOILERPLATE = re.compile(r"^\s*(Kontoauszug \d+/\d{4}|Seite \d+ von \d+|Girokonto \d+|"
                         r"Datum\s+Erläuterung|Deutsche Kreditbank|Telefon|Telefax|"
                         r"Vorstand|Vorsitzender|USt-ID|Handelsregister|www\.dkb|info@dkb)")


def parse_statement(lines: list[str]) -> tuple[Decimal, Decimal, list[dict]]:
    opening = closing = summary = None
    entries, in_table = [], False
    for index, raw in enumerate(lines):
        text = raw.strip()
        old, new = OPENING.search(raw), CLOSING.search(raw)
        if old and opening is None:
            opening, in_table = money(old[2], True), True
            continue
        if new:
            closing, in_table = money(new[2], True), False
            continue
        if text.startswith("Gesamtumsatzsummen"):
            summary = re.findall(r"-?[\d.]+,\d\d|\b\d+\b", " ".join(lines[index + 1:index + 2]))
            in_table = False
            continue
        if not in_table or not text or BOILERPLATE.match(raw):
            continue
        match = ENTRY.match(raw)
        if match:
            entries.append(dict(row=index + 1, bd=match[1], cp=match[2].strip(),
                                amt=money(match[3], True), pur=[], vd=""))
        elif entries:
            value = WERT.search(text)
            if value and not entries[-1]["vd"]:
                entries[-1]["vd"] = value[1]
            entries[-1]["pur"].append(text)
    if opening is None or closing is None or not summary or len(summary) != 4:
        raise ValueError("DKB balance or cash summary missing")
    if opening + sum((e["amt"] for e in entries), Decimal(0)) != closing:
        raise ValueError("DKB balance reconciliation failed")
    debits = [e["amt"] for e in entries if e["amt"] < 0]
    credits = [e["amt"] for e in entries if e["amt"] > 0]
    if (sum(debits, Decimal(0)), len(debits), sum(credits, Decimal(0)), len(credits)) != (
            money(summary[0], True), int(summary[1]), money(summary[2], True), int(summary[3])):
        raise ValueError("DKB cash totals/counts failed")
    for entry in entries:
        if entry["pur"]:
            entry["cp"] += " " + entry["pur"].pop(0)
    return opening, closing, entries


if __name__ == "__main__":
    pdf_cli(parse_statement)
