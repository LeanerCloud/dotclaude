# Purpose rules and confirmed overrides

Tagging should answer what the payment was for while retaining the evidence and uncertainty. A transaction's economic purpose, business share and legal deductibility are separate decisions. A Personal category can still need review for a personal tax deduction; Business is not proof of eligibility.

## Build rules from the actual records

Use the example `scripts/categorize.py`, `scripts/examples/rules.json` and `scripts/examples/overrides.json` as starting points in the private workspace. The sample merchants and IDs are fictional. Replace them with rules justified by the owner's records; do not treat the examples as a ready-made tax policy.

The overrides example intentionally contains a demonstration ID that will not match real data. Replace it with evidenced real IDs, or use an empty object `{}` when there are no confirmed overrides. After adapting the private copies, an example invocation from the Taxes workspace is:

```text
python3 scripts/categorize.py 2025/transactions/ing-v1.csv 2025/transactions/ing-tagged-v1.csv --rules rules/purpose.json --overrides rules/overrides.json
```

Keep `bank_common.py` beside the categorizer. Run the rules against synthetic cases first; then tag each reconciled input or an explicitly verified merged ledger, using override files scoped to that input.

Inspect existing categories first. The original workflow used `Income:<purpose>`, `Business:<purpose>`, `Tax:<purpose>`, `Personal:<purpose>`, `Transfer:<purpose>` and `Review:<reason>`. Reuse that format if it fits the workbook, or keep the owner's established schema. Add a normalized vendor separately from the raw counterparty/purpose. Income tax payments, for example, belong to a tax-payment schedule, not operating expenses.

Prefer narrow matches against the relevant field. A rule can match the counterparty, payment purpose or their combined text and constrain debit/credit direction. Use invoice references, confirmed contracts, transfer pair evidence and owner confirmations where merchant-only text is ambiguous. A processor name alone does not identify the supplier; all airlines/hotels are not necessarily business travel; all incoming own-account transfers are not revenue. Do not put a broad Personal fallback after uncertain card payments just to reduce the review count.

The example evaluates matching rules in configured order and exposes conflicting matches as `Review:Ambiguous`, rather than silently letting a broad first match win. Refine overlapping rules until their scopes express the actual decisions. A refund can reverse a known expense category, but match it to the original purchase before approving its purpose or sign-specific rule.

## Confirmations survive reruns

Store transaction-specific overrides keyed by the stable ID with category, vendor and evidence. Evidence can be a private invoice path, linked transfer ID or dated owner confirmation. Limit the override to the identified payment(s), period or trip. A confirmation for seven marketing payments through a processor is not authorization to classify every processor transfer as marketing.

Apply explicit overrides ahead of generic rules. Preserve prior confirmed override metadata when rerunning the example, and resolve conflicting confirmations explicitly rather than replacing them accidentally. Overrides do not validate tax treatment on their own. When source bytes or aliases change, map old IDs to new records with evidence; do not discard confirmations or apply them by approximate amount alone.

Treat an override file containing IDs absent from the input as a coverage problem to resolve, not an instruction to ignore them. Scope override files to the intended input or provide an explicit evidenced mapping before running. Confirmed decisions missing from a regenerated ledger must be surfaced before publishing.

Maintain raw signed amount and currency without changing them during tagging. Leave FX conversion to the documented rate method. Noncash travel allowances, depreciation and platform schedules must retain their own provenance and bypass generic bank rules that would reinterpret them.

## Review and publish

Unknown or conflicting purposes remain in a review queue, with source row, account, date, signed amount, currency, raw description, candidate rules and missing evidence. Search relevant authorized invoices/email first; ask the owner a concise grouped question only when those records cannot resolve a material purpose. Do not present uncertain matches as confirmed just because the script assigns a label.

Write a new tagged file, compare row counts and cash totals with the input, and inspect changes in previously confirmed categories before using it downstream. Group review counts and raw totals by currency; do not add unlike currencies. Test representative rule matches, false positives, refunds, equal payments, conflicting rules, unknowns and override persistence with the shipped synthetic tests after adapting rules.

Publish tax totals only after the reconciliation and eligibility checks in SKILL.md. When the task is audit-only, report proposed rules/corrections without rewriting the user's workbook or historical ledger.
