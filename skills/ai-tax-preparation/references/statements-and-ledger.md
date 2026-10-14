# Statement and ledger workflow

## Normalize without losing provenance

Prefer existing parsers and schemas. Where needed, retain source file, source row/page or transaction identifier, account alias, booking date, value date, raw counterparty, purpose, signed amount, currency and reported balance. Add category, normalized vendor and reporting-currency amount as derived fields. Keep classification evidence or override provenance in the ledger or a linked decision file.

Use exact decimal arithmetic. Preserve raw amount and separately reported fees even when normalized cash includes them. Establish provider sign and fee conventions from the export and balance changes; do not assume all providers behave alike.

Separate the cash reconciliation amount from taxable revenue and deductible expense. If an incoming wire arrives net of a fee, use invoice/remittance evidence to determine whether gross revenue and a separate fee are required. Do not infer gross revenue from an apparently round invoice amount or subtract a fee a second time from already-net cash.

Keep original dates. Use the applicable recognition rule for the reporting year. Document coverage before filtering multi-year exports so missing months and year-boundary payments remain visible.

## Extract and reconcile PDFs

Use layout-aware extraction, then inspect the original page or render when columns, wraps or scans are ambiguous. Separate transactions from certificates, summaries, interest-rate illustrations, headers and footers. Scans require visual review or OCR; filesystem creation dates do not prove the tax year.

For each complete statement, verify `opening balance + signed cash movements = closing balance` in its currency. Check the next statement's opening balance, credit/debit counts and subtotals when provided. Reconcile the full statement before annual filtering.

Do not deduplicate merely because date, counterparty and amount match: genuine repeated payments occur. Balance continuity, transaction IDs and source position can distinguish them. Zero-amount entries may appear in detail but not the bank's count of cash movements. Amount-only continuation lines can be real fees and must not be discarded for lacking a payee. Distinguish footnote markers attached to amounts from monetary digits.

Observed German statement patterns are clues to check against the actual format:

- ING: booking and value-date lines may differ; wrapped descriptions can absorb the next transaction or boilerplate. Savings statements may append an interest calculation table that is not a list of postings.
- DKB: separate debit/credit columns and turnover subtotals provide controls. Zero-value settlement rows may be excluded from those subtotals.
- Revolut: read actual currency and product columns, not filename guesses. Combined exports can overlap currency-specific exports. Reverted entries, separate fees and multiple account products need explicit handling.

## Overlapping exports and transfers

Prove whether one export is a subset of another using IDs where available, or sufficiently complete comparisons and multiplicity. Select one authoritative representation of overlapping transactions. Deduplicate across redundant representations, preserving repeated occurrences within the authoritative export. Do not erase genuine duplicate-looking transactions or merge independent account products.

Determine which status values represent settled cash movements. Keep excluded-state counts and amounts visible for reconciliation. In Revolut exports with separately positive Fee values, cash may be `Amount - Fee`; validate against the balance chain before using it.

Use a stable transaction ID or documented composite key for repeat imports and classification overrides. Include occurrence/source position where identical legitimate transactions exist. Re-running the pipeline must not duplicate rows, remove confirmed overrides or apply overrides to newly encountered payments merely because the merchant matches. Compare before/after row counts, controls and changed classifications before writing downstream outputs.

A processor payment does not identify the final recipient. Use a linked statement, receipt, reference or owner confirmation. Record confirmation against the identified transactions; keep unrelated transfers unresolved.

## Currency conversion

Never add unlike currencies without conversion. Present raw totals by currency and reporting-currency totals separately. Record rate source, quotation direction, date, rounding and method. For foreign-currency units per EUR, divide to convert into EUR.

Research and agree the applicable method before reusing an earlier workflow. The source conversation used daily ECB rates, with the preceding published date on non-publication days, for bank transactions; a separate platform schedule used monthly-average ECB rates to reproduce existing bookkeeping. These are observed methods, not universal tax rules. Do not silently mix methods or assume the average inverse equals the inverse average.

## Revenue outside bank statements

Inventory platforms, wallets, invoicing systems and other channels. Bank-only totals can omit revenue retained elsewhere. A platform export and its later payout can also count the same income twice.

Distinguish gross sales, platform fees, refunds, net earnings, payouts and wallet-to-bank transfers. Choose authoritative records for the accounting method and reconcile other representations. Use certificates when they provide authoritative annual figures. Purchases from a platform are not income from it.

A multi-year export belongs to its covered periods, regardless of its folder. Repeated equal monthly values do not prove a forecast: inspect the export and reporting cutoff. Synthetic dates used in an agreed monthly schedule must be labelled, not presented as payout dates.

## Audit existing bookkeeping

Inspect formulas and covered columns, not only totals. Check new vendors omitted from sums, missing months, numeric strings, amounts formatted as dates, stale rates, duplicated columns and USD amounts treated as EUR.

Audit findings are hypotheses until reproduced against sources. Quantify confirmed errors separately from uncertain classifications. Build a discrepancy bridge across coverage, dates, currency, fees, gross/net, refunds and categories. Preserve manual and derived figures until the cause is established. Keep residuals and missing evidence visible; do not repair historical sheets during an audit-only request.

For each year, report revenue difference and expense difference separately, then calculate profit effect as change in revenue minus change in expenses. State the comparison baseline and sign convention. Do not combine currencies, compare a full year with YTD, or describe a near-match as proof of the cause.
