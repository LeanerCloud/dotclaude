# Working records and handoff

Use these records for a preparation spanning several sources or sessions. Adapt existing files instead of creating duplicate reports. Keep identifiers and financial details in the owner's private workspace; reusable skill files contain procedures only.

## Coverage inventory

Record source/account alias, document path, actual covered period, currency/product, statement opening/closing controls, extraction status and missing documents. Include certificates, wallets/platforms and income channels that do not pay into inventoried banks. Label each year as full-year or YTD with an explicit cutoff.

A certificate issued in the next calendar year can belong to the filing year. A bank statement crossing year-end requires full-statement reconciliation followed by recognition-year filtering. Account for each inventoried source as included, overlapping, out of period, or unresolved; do not infer completeness from the number of downloaded files.

## Ledger and decisions

Reuse the existing schema. A practical minimum is:

| Layer | Fields or linked evidence |
|---|---|
| Source | Source file and row/page or logical CSV record, provider transaction ID or stable key, account/product alias |
| Cash | Booking/value dates, raw purpose/counterparty, signed cash amount, currency, reported balance, fees |
| Derived | Reporting amount, FX date/source/method, category, vendor, linked transfer/refund IDs |
| Treatment | Business share, eligibility evidence, tax-year attribution, inclusion/exclusion reason, form mapping |

Not every source supplies every field. Preserve unavailable values as missing and describe the resulting verification limit. Keep a rule-generated category distinct from an owner-confirmed override.

For a consequential decision, record the affected transaction IDs/period, owner-supplied fact, evidence reference, treatment and reason, confirmation date, and anything it supersedes. Separate confirmed facts from legal conclusions still requiring research. Examples of bounded decisions: identified processor transfers pay a marketing contractor; a particular trip is personal; confirmed travel dates support an allowance schedule. None is a blanket merchant rule.

For researched treatment, record the jurisdiction, effective tax year, official source link and relevant passage, calculation inputs, eligibility conditions and unresolved evidence. Record retrieval date where the source can change. Do not preserve a previous assistant's unsupported claim as an established rule.

## Reconciliation and discrepancy report

Show the scope and result of the independent controls:

- Original statement balance equations and continuity by account/currency; explain discontinuities and unavailable balances.
- Extracted/normalized row counts, settled and excluded states, overlaps, fees, transfers and reporting-currency conversion.
- Read-back workbook totals and saved return fields against the ledger plus separately documented tax adjustments.

Describe exact coverage, rather than saying only "checked three times". Classifying every transaction does not prove eligibility; matching a net balance does not prove every gross movement was extracted.

Bridge disagreements using signed components for missing sources, timing, FX, gross/net, refunds, classification, formula errors and noncash adjustments. Each component needs source evidence; keep unexplained residuals separate. A suspected cause can be shown as a hypothesis, but must not be counted as a confirmed correction.

If useful, summarize monthly expenses by vendor/category and income by channel, with annual totals. Preserve cash controls independently of tax-only deductions. Read formulas after writing, including the last vendor column and any added year block.

## Draft field map

Track section/field, source ledger rows or certificate, raw eligible amount, business/parental allocation, adjustment, entered amount, software-applied reduction, saved read-back value and remaining conditions. Use actual labels from the selected application rather than assumed field numbers.

Distinguish an intentional owner-requested zero from verified absence of reportable income. Mark carried-forward figures as verified, replaced or unresolved. Keep tax payments in a separate schedule with payment date, tax type, assessed year, reference evidence and where the software counts them.

For each edited page, a short update can show intended change, saved value and verification result. Do not expose identifiers in routine progress messages. Resolve a mismatch between calculated and displayed totals before treating the page as finished.

## Handoff and next-year reuse

Finish with:

- Completed outputs and exact private paths, covered year/cutoff, pipeline/rate-source versions where relevant.
- Verified controls and material limitations, unresolved transactions and missing certificates/agreements.
- Draft application/year, last saved calculation and preview path/version, warnings and filing status.
- Owner's next action and any separately authorized action still pending.

Keep a restart note containing the current stage, validated outputs, decisions that must survive reruns, active assumptions, and next step. An authenticated tab or a successful data check is not evidence that the return was filed.

For next year, reuse parsers, schemas and workbook layout after checking actual formats and account coverage. Refresh tax law, FX data, certificates, income sources, family/household facts and authorizations. Previous-year allowances, allocations, carried-forward assets and software defaults require review rather than automatic copying.
