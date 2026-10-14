# Bank downloads and parser adaptation

## Discover and download through computer use

Start with the bank URL/tab the owner identifies. If only a bank name is known, verify its official domain before opening it. Inspect current tabs first; select the correct institution, account and authenticated user without collecting credentials. Ask the owner to log in or complete MFA/security challenges when necessary. Record the stage to resume after a timeout. Do not bypass blocked pages or change permission settings.

Discover the institution's actual navigation: documents/mailbox, statements, annual tax certificates, export/download and account/product selectors. Use fresh UI state and current labels; session-specific element IDs, screen coordinates and previous-session URLs are not reusable instructions. Reacquire elements after navigation. Download operations should be serialized within the same browser session to avoid selecting the wrong account or save dialog.

For each authorized account/product and needed currency:

1. Inspect the document list, date filters, pagination and archive ranges. Determine the actual coverage required for the filing year or YTD cutoff, including statements issued in the next year that contain filing-year transactions. Also collect available annual tax/interest certificates for the relevant year, not merely documents with that year's filename.
2. Download original PDFs and native CSV exports when available. Use the existing private workspace, or discover a suitable private Taxes/<year>/<institution> destination when none exists. Create only needed folders; record actual saved paths. Preserve source filenames unless an additive disambiguation is needed to prevent overwriting.
3. After saving, verify the file exists, is nonempty, has the expected PDF/CSV content and matches the selected account and covered period. A completed click, browser download badge or default save path is not sufficient evidence. Check each archive page and account selector against the inventory so unfinished pagination does not look complete.
4. Identify redundant downloads by content hashes and coverage. Retain originals and duplicates; record which representation is authoritative for parsing. A next-year statement can legitimately support both years without counting its transactions twice within a year.

Keep a small download manifest with account alias, source path, hash, document type, covered dates, currency/product and extraction status. Put real account identifiers only in private records. Do not click transfer, payment, account closure, direct-debit changes or filing buttons while collecting documents.

If exports arrive by email, search only the authorized inbox and relevant sender/date range, then verify the attachment's contents. If statements were AirDropped or manually downloaded, inspect likely existing files rather than asking for them again. Do not assume a CSV saved in one year's folder covers only that year.

## Choose and adapt the examples

The `scripts/` directory ships examples derived from the prior session's ING/DKB PDF parsers, Revolut normalizer and rule-based categorizer. They contain no private account IDs or owner-specific merchant mappings. Read the selected script and its `--help` output before using it. Copy the needed examples into the private workspace's scripts folder; adapt those copies rather than editing the installed skill for each return.

Runtime dependencies are Python 3.10 or newer and Poppler's `pdftotext` for PDF extraction. Use the available PDF skill/tooling when visual inspection or OCR is needed. Inspect local dependencies before installing anything. The examples use standard-library Decimal arithmetic; no banking credentials are passed to scripts.

Example invocations from a private Taxes workspace after copying the selected scripts, preparing statement-only scratch directories and creating output parent folders:

```text
python3 scripts/parse_ing_statements.py scratch/ing-primary 2025/transactions/ing-v1.csv --year 2025 --account ing-primary
python3 scripts/parse_dkb_statements.py scratch/dkb-primary 2025/transactions/dkb-v1.csv --year 2025 --account dkb-primary
python3 scripts/convert_revolut.py 2025/transactions/revolut-v1.csv 2025/Revolut/authoritative.csv --year 2025 --account revolut-primary
```

Substitute the established filing year, manifest-selected inputs and aliases. Output names are deliberately new; existing outputs are never overwritten. Keep `bank_common.py` beside the copied entrypoint scripts because they import it. Use ordinary `python3` invocation for these sibling imports, rather than copying the original session's isolated-mode command blindly.

`python3 -m pytest scripts/tests` runs the shipped synthetic suite from the skill root when pytest is available. The tests are development checks; pytest is not a runtime dependency for processing records.

| Actual input | Starting point | What to inspect before adapting |
|---|---|---|
| ING Kontoauszug PDF | `scripts/parse_ing_statements.py` | Alter/Neuer Saldo, booking/Valuta pairs, page headers/footers, amount footnotes and savings interest illustrations |
| DKB Kontoauszug PDF | `scripts/parse_dkb_statements.py` | Kontostand opening/closing, debit/credit signs, Gesamtumsatzsummen counts/sums, wrapped references and zero settlements |
| Revolut account-statement CSV | `scripts/convert_revolut.py` | Actual headers, date order, product/currency separation, states, Amount/Fee convention, reported balances and overlapping exports |
| Another provider or changed format | Closest example's schema and controls | Actual statement structure and native export specification; replace provider-specific parsing rather than pretending the format matches |

Choose an account alias that distinguishes each actual account/product; do not embed its number in reusable code. For PDFs, inspect layout-preserving text and the rendered original across representative months, first/last pages and year-end statements. Edit parsing patterns, boundaries and continuation handling only against observed source layouts. Do not identify transactions solely by an amount regex or discard continuation lines just because they lack a payee.

The PDF example input directory must contain only the selected statements for one account, not annual certificates or duplicate downloads. Use a scratch directory with copies of the inventoried authoritative statements, or adapt the source selection to the verified manifest. Keep certificates available for their separate extraction and tax controls; do not remove them from the originals folder to satisfy a parser.

The PDFs in the original workflow were German-language layouts. These examples do not claim to parse every ING/DKB country or format. Detect unsupported layouts, missing controls, scanned text and ambiguous debit/credit signs visibly. Never compensate for a parsing error by altering a balance or inventing a booking.

The ING example refuses an empty extraction unless an explicit no-activity marker accompanies the recognized table. Its `Keine Umsätze` marker is demonstrated synthetically, not established as every bank version's wording. Adapt it only to an observed no-activity control in the original document. Equal opening and closing balances alone can hide offsetting missed transactions.

Revolut's example requires chronological ledger order within each product/currency and preserves same-day occurrence order. Its original fee convention is signed Amount minus a nonnegative Fee; negative fees or a different convention require evidenced adaptation. It accepts fiat-style amounts with at most two decimal places, not arbitrary crypto precision. Defaults include COMPLETED and exclude REVERTED; other states need a verified explicit JSON mapping. A reported excluded-row amount is an audit control, not settled cash.

## Prove the adapted parser

Run the shipped synthetic tests after editing the workspace copies, then verify against actual originals. The agent performs this work; do not give the owner a script-editing task list.

- Verify each full source before annual filtering. Opening plus signed movements must equal closing; bank counts and debit/credit subtotals add independent evidence. A zero-amount settlement can be present but excluded from cash counts. Savings interest illustrations are not postings.
- Compare sampled rows visually to the original, including long wraps, foreign fees, refunds and year-boundary entries. A balanced extraction can still contain offsetting mistakes.
- Confirm account/currency continuity and complete annual coverage across sources. The examples' per-source checks do not independently establish missing months or a CSV's first opening balance. Keep these limits in the report.
- Use new output paths. On failure, retain originals and any inspection artifacts; stop downstream publication and diagnose the exact document. Do not omit the failed source and label the remainder complete.
- Check row counts, raw currency totals and duplicate/overlap decisions before merging providers or publishing sheets. Retain legitimate equal payments. Revolut's conservative overlap rejection calls for selecting an authoritative export, not weakening the check until concatenation succeeds.

Keep classification, FX conversion and tax adjustments separate from source parsing. When available, Revolut provider transaction IDs supply the identity within an account/product/currency alias. Otherwise example IDs bind source bytes, source occurrence and account alias: the occurrence is an extracted text line for PDFs or logical record position for CSVs. These fallback IDs survive identical-source reruns, but a regenerated export or changed alias needs an explicit mapping before reusing overrides. Preserve the old derived ledger and decisions when regenerating.
