import csv
import subprocess
import sys
from argparse import Namespace
from pathlib import Path

import pytest

SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS))
import bank_common as common
import categorize
import convert_revolut as revolut
import parse_dkb_statements as dkb
import parse_ing_statements as ing

ING = """Alter Saldo 100,00
Neuer Saldo 79,00
Valuta
31.12.2024 Fictional shop  -10,00
31.12.2024 Invoice example
01.01.2025 Fictional shop  -10,00
01.01.2025 Invoice example
02.01.2025 Fee  -1,00
02.01.2025 Fee continuation
01.01.2025 bis 02.01.2025 1,250% Zins  0,80
""".splitlines()
DKB = """Kontostand am 31.12.2024, Auszug Nr. 1 100,00
 01.01.2025 Lastschrift  -10,00
 Fictional shop
 Wert: 01.01.2025
 02.01.2025 Abrechnung  0,00
 Fee-only continuation
 03.01.2025 Gutschrift  5,00
 Fictional refund
Kontostand am 03.01.2025 um 12:00 Uhr 95,00
Gesamtumsatzsummen
-10,00 1 5,00 1
""".splitlines()


def export(path, rows):
    fields = ["State", "Product", "Currency", "Amount", "Fee", "Balance", "Completed Date",
              "Started Date", "Type", "Description"]
    with path.open("w", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=fields)
        writer.writeheader()
        for product, currency, amount, fee, balance, date in rows:
            writer.writerow(dict(zip(fields, ["COMPLETED", product, currency, amount, fee, balance,
                                               date, date, "Card", "Fictional shop"])))
    return path


def test_ing_interest_and_year_filter_preserve_full_balance(tmp_path, monkeypatch):
    (tmp_path / "statement.pdf").write_bytes(b"fictional PDF")
    monkeypatch.setattr(common, "pdf_text", lambda path: ING)
    output = tmp_path / "out.csv"
    common.run_pdf(Namespace(pdf_dir=tmp_path, out_csv=output, year="2025", account="demo"), ing.parse_statement)
    rows = list(csv.DictReader(output.open()))
    assert [r["amount"] for r in rows] == ["-10.00", "-1.00"]
    assert rows[0]["balance_after"] == "80.00"
    assert rows[1]["balance_after"] == "79.00"
    assert rows[0]["transaction_id"] != rows[1]["transaction_id"]
    with pytest.raises(FileExistsError):
        common.run_pdf(Namespace(pdf_dir=tmp_path, out_csv=output, year="2025", account="demo"), ing.parse_statement)


def test_dkb_totals_counts_and_zero_settlement():
    opening, closing, rows = dkb.parse_statement(DKB)
    assert len(rows) == 3
    assert rows[1]["cp"].endswith("Fee-only continuation")
    assert opening - 5 == closing
    with pytest.raises(ValueError, match="totals/counts"):
        dkb.parse_statement(DKB[:-1] + ["-10,00 2 5,00 1"])


def test_pdf_late_failure_leaves_no_output(tmp_path, monkeypatch):
    (tmp_path / "a.pdf").write_bytes(b"a")
    (tmp_path / "b.pdf").write_bytes(b"b")
    monkeypatch.setattr(common, "pdf_text", lambda p: ING if p.name == "a.pdf" else
                        [line.replace("79,00", "78,00") for line in ING])
    output = tmp_path / "out.csv"
    with pytest.raises(ValueError, match="reconciliation"):
        common.run_pdf(Namespace(pdf_dir=tmp_path, out_csv=output, year="2025", account="demo"), ing.parse_statement)
    assert not output.exists()


def test_no_source_and_missing_controls_fail(tmp_path):
    with pytest.raises(ValueError, match="no PDF"):
        common.run_pdf(Namespace(pdf_dir=tmp_path, out_csv=tmp_path / "out", year="2025", account="demo"), ing.parse_statement)
    with pytest.raises(ValueError):
        dkb.parse_statement(DKB[:-2])
    with pytest.raises(ValueError):
        ing.parse_statement(["Valuta"])


def test_revolut_equal_payments_fee_chain_mixed_ledgers_and_year_boundary(tmp_path):
    path = export(tmp_path / "source.csv", [
        ("Current", "EUR", "-10", "1", "89", "2024-12-31"),
        ("Savings", "EUR", "5", "0", "1005", "2025-01-01"),
        ("Current", "USD", "-10", "0", "40", "2025-01-01"),
        ("Current", "EUR", "-10", "1", "78", "2025-01-01"),
        ("Current", "EUR", "-10", "1", "67", "2025-01-01"),
        ("Current", "EUR", "0", "2", "65", "2025-01-02")])
    rows = revolut.convert([path], "2025", "demo", {"COMPLETED": "include"})
    assert len(rows) == 5
    assert len({r["transaction_id"] for r in rows}) == 5
    assert [r["amount"] for r in rows][-3:] == ["-11.00", "-11.00", "-2.00"]
    assert len({r["account"] for r in rows}) == 3


def test_revolut_duplicate_overlap_and_bad_chain_refused(tmp_path):
    a = export(tmp_path / "a.csv", [("Current", "EUR", "-1", "0", "9", "2025-01-01")])
    b = export(tmp_path / "b.csv", [("Current", "EUR", "-2", "0", "7", "2025-01-01")])
    with pytest.raises(ValueError, match="duplicate"):
        revolut.convert([a, a], "2025", "demo", {"COMPLETED": "include"})
    with pytest.raises(ValueError, match="overlapping"):
        revolut.convert([a, b], "2025", "demo", {"COMPLETED": "include"})
    bad = export(tmp_path / "bad.csv", [("Current", "EUR", "-1", "0", "9", "2024-12-30"),
                                             ("Current", "EUR", "-1", "0", "5", "2024-12-31")])
    with pytest.raises(ValueError, match="chain"):
        revolut.convert([bad], "2025", "demo", {"COMPLETED": "include"})


def row(identity="fictional-1"):
    return dict(transaction_id=identity, counterparty="Fictional shop", purpose="Invoice DEMO",
                amount="-10.00", currency="USD")


def rule(identity, category, field="counterparty", sign="debit"):
    return dict(id=identity, category=category, vendor="Fictional shop", field=field,
                regex="Fictional|Invoice", sign=sign)


def test_rules_order_conflicts_sign_and_unknown():
    rules = [rule("first", "Business:Software"), rule("second", "Business:Software", "purpose")]
    tagged = categorize.classify([row()], rules, {})[0]
    assert tagged["rule_ids"] == "first;second"
    assert tagged["amount"] == "-10.00" and tagged["currency"] == "USD"
    ambiguous = categorize.classify([row()], rules + [rule("third", "Personal:Shopping")], {})[0]
    assert ambiguous["category"] == "Review:Ambiguous"
    assert categorize.classify([row()], [rule("credit", "Income:Consulting", sign="credit")], {})[0]["category"] == "Review:Unidentified"


def test_bounded_override_persistence_conflict_and_unmatched():
    override = {"fictional-1": dict(category="Business:Software", vendor="Demo", evidence="Invoice DEMO confirmed")}
    rows = categorize.classify([row(), row("fictional-2")], [], override)
    assert rows[1]["category"] == "Review:Unidentified"
    rerun = categorize.classify(rows, [rule("new", "Personal:Shopping")], {})
    assert rerun[0]["category"] == "Business:Software"
    assert rerun[0]["classification_evidence"] == "Invoice DEMO confirmed"
    with pytest.raises(ValueError, match="conflicting"):
        categorize.classify(rerun, [], {"fictional-1": dict(category="Personal:X", vendor="Demo", evidence="Other")})
    with pytest.raises(ValueError, match="unmatched"):
        categorize.classify([row()], [], {"unmatched": override["fictional-1"]})


def test_exclusive_writer(tmp_path):
    output = tmp_path / "out.csv"
    output.write_text("preserve")
    with pytest.raises(FileExistsError):
        common.write_csv(output, [])
    assert output.read_text() == "preserve"


def test_revolut_late_failure_cli_does_not_create_destination(tmp_path):
    good = export(tmp_path / "good.csv", [("Current", "EUR", "-1", "0", "9", "2025-01-01")])
    bad = export(tmp_path / "bad.csv", [("Current", "USD", "-1", "0", "9", "2025-02-01"),
                                            ("Current", "USD", "-1", "0", "5", "2025-02-02")])
    output = tmp_path / "out.csv"
    result = subprocess.run([sys.executable, "-B", str(SCRIPTS / "convert_revolut.py"),
                             str(output), str(good), str(bad), "--year", "2025", "--account", "demo"],
                            capture_output=True, text=True)
    assert result.returncode != 0 and "chain" in result.stderr
    assert not output.exists()


def test_revolut_unknown_states_need_explicit_mapping(tmp_path):
    path = export(tmp_path / "unknown.csv", [("Current", "EUR", "-1", "0", "9", "2025-01-01")])
    path.write_text(path.read_text().replace("COMPLETED", "UNFAMILIAR"))
    with pytest.raises(ValueError, match="unknown"):
        revolut.convert([path], "2025", "demo", {"COMPLETED": "include"})
    assert len(revolut.convert([path], "2025", "demo", {"UNFAMILIAR": "include"})) == 1


def test_provider_id_survives_regenerated_source(tmp_path):
    a = export(tmp_path / "a.csv", [("Current", "EUR", "-1", "0", "9", "2025-01-01")])
    with a.open(newline="") as handle:
        reader = csv.DictReader(handle)
        fields, records = reader.fieldnames, list(reader)
    with a.open("w", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=fields + ["ID"])
        writer.writeheader()
        writer.writerow(records[0] | {"ID": "fictional-provider-001"})
    b = tmp_path / "b.csv"
    b.write_text(a.read_text().replace("Fictional shop", "Fictional shop amended description"))
    first = revolut.convert([a], "2025", "demo", {"COMPLETED": "include"})
    second = revolut.convert([b], "2025", "demo", {"COMPLETED": "include"})
    assert first[0]["transaction_id"] == second[0]["transaction_id"]


def test_tagging_cli_overwrite_rejected_and_review_totals_by_currency(tmp_path):
    source, output = tmp_path / "source.csv", tmp_path / "out.csv"
    records = [row(), row("fictional-2") | {"currency": "EUR", "amount": "5.00"}]
    common.write_csv(source, records, list(records[0]))
    rules, overrides = tmp_path / "rules.json", tmp_path / "overrides.json"
    rules.write_text("[]")
    overrides.write_text("{}")
    command = [sys.executable, "-B", str(SCRIPTS / "categorize.py"), str(source), str(output),
               "--rules", str(rules), "--overrides", str(overrides)]
    result = subprocess.run(command, capture_output=True, text=True)
    assert result.returncode == 0
    assert "review EUR: 1 rows, signed raw total 5.00" in result.stdout
    assert "review USD: 1 rows, signed raw total -10.00" in result.stdout
    saved = output.read_bytes()
    result = subprocess.run(command, capture_output=True, text=True)
    assert result.returncode != 0 and "overwrite" in result.stderr
    assert output.read_bytes() == saved


def test_write_failure_reports_incomplete_artifact():
    class BrokenFile:
        def __enter__(self):
            return self

        def __exit__(self, *args):
            pass

        def write(self, value):
            raise OSError("synthetic disk failure")

    class BrokenPath:
        def open(self, *args, **kwargs):
            return BrokenFile()

    with pytest.raises(RuntimeError, match="incomplete artifact"):
        common.write_csv(BrokenPath(), [])


def test_ing_missing_table_rejects_offsetting_extraction_and_valid_zero_table_passes():
    text = ["Alter Saldo 100,00", "Neuer Saldo 100,00", "01.01.2025 Demo  -10,00",
            "02.01.2025 Demo  10,00"]
    with pytest.raises(ValueError, match="table marker"):
        ing.parse_statement(text)
    assert ing.parse_statement(["Alter Saldo 100,00", "Neuer Saldo 100,00", "Valuta", "Keine Umsätze"])[2] == []
    with pytest.raises(ValueError, match="no-activity"):
        ing.parse_statement(["Alter Saldo 100,00", "Neuer Saldo 100,00", "Valuta"])
    with pytest.raises(ValueError, match="booking-like"):
        ing.parse_statement(text[:2] + ["Valuta"] + [line.replace("  ", " ") for line in text[2:]])


@pytest.mark.parametrize("invalid", ["NaN", "Infinity", "invalid", "0.001"])
def test_confirmed_override_does_not_bypass_raw_amount_validation(invalid):
    override = {"fictional-1": dict(category="Business:Software", vendor="Demo", evidence="Invoice DEMO")}
    with pytest.raises(ValueError):
        categorize.classify([row() | {"amount": invalid}], [], override)


def test_excluded_states_report_available_raw_amounts_and_missing_values(tmp_path, capsys):
    path = export(tmp_path / "states.csv", [("Current", "EUR", "-1", "0", "9", "2025-01-01"),
                                              ("Current", "USD", "-3", "0", "7", "2025-01-01"),
                                              ("Current", "USD", "-4", "0", "3", "2025-01-02")])
    with path.open(newline="") as handle:
        reader = csv.DictReader(handle)
        fields, records = reader.fieldnames, list(reader)
    records[1]["State"] = records[2]["State"] = "REVERTED"
    records[2]["Amount"] = ""
    with path.open("w", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=fields)
        writer.writeheader()
        writer.writerows(records)
    result = revolut.convert([path], "2025", "demo", {"COMPLETED": "include", "REVERTED": "exclude"})
    assert len(result) == 1
    log = capsys.readouterr().out
    assert "excluded Current/REVERTED/USD: 2 rows" in log
    assert "raw amount subtotal -3.00" in log and "absent/unverified amounts 1" in log
    assert "not cash effects" in log


def test_empty_currency_and_malformed_explicit_override_fail():
    with pytest.raises(ValueError, match="currency"):
        categorize.classify([row() | {"currency": ""}], [], {})
    with pytest.raises(ValueError, match="malformed"):
        categorize.classify([row()], [], {"fictional-1": {}})


@pytest.mark.parametrize("script", ["categorize.py", "convert_revolut.py"])
@pytest.mark.parametrize("malformed", ["duplicate_header", "extra_field"])
def test_cli_rejects_ambiguous_csv_shape_before_destination_creation(tmp_path, script, malformed):
    source, output = tmp_path / "input.csv", tmp_path / "out.csv"
    if script == "categorize.py":
        source.write_text("transaction_id,counterparty,purpose,amount,currency\nfictional-id,Demo,Invoice,-100.00,EUR\n")
        rules, overrides = tmp_path / "rules.json", tmp_path / "overrides.json"
        rules.write_text("[]")
        overrides.write_text("{}")
        options = ["--rules", str(rules), "--overrides", str(overrides)]
        duplicate = "amount"
    else:
        export(source, [("Current", "EUR", "-100.00", "0", "900", "2025-01-01")])
        options = ["--year", "2025", "--account", "demo"]
        duplicate = "Amount"
    lines = source.read_text().splitlines()
    if malformed == "duplicate_header":
        lines[0] += "," + duplicate
    lines[1] += ",-1.00"
    source.write_text("\n".join(lines) + "\n")
    arguments = ([str(source), str(output)] if script == "categorize.py" else [str(output), str(source)])
    command = [sys.executable, "-B", str(SCRIPTS / script), *arguments, *options]
    result = subprocess.run(command, capture_output=True, text=True)
    assert result.returncode != 0
    assert ("duplicate CSV headers" if malformed == "duplicate_header" else "extra fields") in result.stderr
    assert not output.exists()
    output.write_text("preserved prior output")
    result = subprocess.run(command, capture_output=True, text=True)
    assert result.returncode != 0 and "overwrite" in result.stderr
    assert output.read_text() == "preserved prior output"
