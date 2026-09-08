---
name: cristi-voice
description: Cristian Magherusan-Stanciu's writing voice as a measured fingerprint plus a de-AI
  checklist. Invoke when writing or reviewing anything published under his name or LeanerCloud's -
  website and landing-page copy, FAQs, LinkedIn posts, newsletters, blog posts, product pages - or
  when rewriting AI-sounding prose so it reads like him.
---

# Cristian's voice

The target is prose that reads like a competent engineer explaining his work: long, plain, additive
sentences, contractions everywhere, specific services and dollar figures instead of adjectives, and
no theatre. The test is not an AI detector (they flag everything, and optimizing against them just
produces a different kind of artificial prose). The test is whether a skeptical technical reader
would pause on a line and think "this was generated".

Everything below was measured, not guessed. Corpus: 27 LinkedIn long-form pieces from the
2022-2023 export (31k words, exported straight from LinkedIn, so definitely his) plus 5 blog posts on
leanercloud.com verified as his by `git log --diff-filter=A`. See "Verify the corpus" before adding
to it.

## 1. The fingerprint

| Metric | His writing | Typical AI marketing copy |
|---|---|---|
| Contractions per 100 words | **1.7 to 1.9** (~90% of contractible forms contracted) | 0.4 to 1.3 |
| Mean words per sentence | **28**, median 25, stdev ~15 | 15 to 22 |
| Sentences of 5 words or fewer | **under 1%** (only sign-offs: "What a mess!", "See you there!") | 5 to 10% |
| Sentences over 40 words | **~15%** (over 60 words: ~4%) | under 5% |
| Semicolons | 3 in 37k words | frequent, joining balanced clauses |
| Em-dashes and en-dashes | **0** (uses a spaced hyphen " - " or "...") | everywhere |
| Rhetorical questions | 0 (his questions are real: "could it be...? I don't know") | common |
| Sentences starting with And/But/So | ~25 per 10k words | rare |

**Contractions he uses**, by frequency: I'm, I'll, I've, it's, that's, wasn't, I'd, don't, didn't,
there's, you're, let's, doesn't, here's, won't, isn't, we're, they're, can't. He almost never writes
"do not", "does not" or "I am" (only for emphasis). Never: ain't, gonna, 'til, it'd/that'd.

**Connectives he uses**: also (82/10k), but (75), so (62), then (25), still (19), going forward,
eventually, besides, unfortunately, it turned out, all in all, in addition. Long sentences accumulate
clauses with "and", "so", "but", "which", "so that".
**Never, in 37k words**: however, moreover, furthermore, additionally, that said, in fact,
basically, ultimately, importantly, notably, in short, in other words, "here's the thing", "the
reality is", "it's worth saying/noting".

**Hedges and intensifiers**: some, a few, a lot, very (19/10k), pretty (12: "pretty much", "pretty
fast", "pretty crude"), a couple of, a bit, a little, probably, quite, really, maybe, a bunch of,
hopefully, "to be honest", "I guess", "not sure", "if I'm not mistaken", "my 2 cents".
**Zero occurrences**: genuinely, precisely, robust, seamless, delve, unlock, empower, foundational,
load-bearing, battle-tested, hand-wavy, clearly, obviously, certainly, absolutely, truly, extremely,
"leverage" as a verb (2 uses, both literal).

**Technical detail**: he names things. 206 numerals and 31 instance types in 37k words
(t3a.2xlarge, m6a.2xlarge, C7g), dollar figures with precision ("$252.28 vs $219.58 monthly in
Virginia", "about $11/month more", "$6k this month"), API and service names (RunInstances, EC2 Fleet,
capacity-optimized-prioritized, awsvpc, ENI trunking). When he generalizes he says why ("I can't
share any numbers"). He gives the mechanism, not the adjective: "the reason was that the allowed
instances configuration used '*' instead of ''".

**Punctuation**: colons introduce lists and screenshots ("here's how it looks so far:").
Parentheses for asides, often with no space before them ("EC2(quite expected"). Comma splices are
part of the voice. Numbers as digits ("6 weeks", "2 calls a day", "20k views"). Exclamation marks
only in sign-offs and small wins. Smileys in newsletters, none in essays. Don't fake his typos.

**Openers and closers** (his own posts): newsletters open "Hello," / "Hi there," / "Another
Friday, another progress report." Essays open with context: "It's been a while since my last post
here, and since then I've been pretty busy." Never a hook, a question, a statistic or a one-liner.
Closers: "That's all for now, thanks for reading so far, have a nice weekend and see you again next
week." signed "-Cristian". Essays end with a "Final words" section and a plain offer of help. A
closing thought is a plain sentence, never a punchline.

**Register**: disagreeing is blunt, first person, with the reason, usually after a concession:
"Don't get me wrong, Kubernetes makes a lot of sense if you're running at scale... But it also has a
relatively steep fixed cost." Uncertainty is plain and specific: "To be honest I have no idea why
this discrepancy, could it be...? I don't know, but I'll experiment." Mistakes are admitted directly:
"In retrospect that warning was a mistake."

A paragraph in his voice states a fact, then the reason, then what he did about it:

> After a few hours of refactoring code and banging my head against the wall it turned out the
> reason was that the allowed instances configuration used "*" instead of "". This made AutoSpotting
> accept all instance types and launched the cheapest available one without looking at the list of
> disallowed instance types. After I noticed this I simply switched the default configuration of
> allowed instance types to "", which makes it work as we expected.

## 2. AI tells to remove, ranked by how often they appear

Hunt these deliberately. Every before/after below is from the leanercloud.com audit; the "after" is
what shipped or what he accepted.

1. **Setup-then-reversal antithesis.** "Not X. Y." / "It isn't A, it's B" / "X was never the
   problem" / "all of the X and none of the Y". The worst and most common offender. Fix: state the
   point once, as a plain clause, or cut.
   - Before: "You don't need a better business case. You need implementation capacity."
     After: "The missing piece is an engineer to do the work"
   - Before: "Not borrowed from the engineering team and pulled onto a feature next sprint. Someone
     whose priority is your priority." After: "An engineer who isn't borrowed from the product team
     and pulled onto a feature next sprint, and whose only priority is your cost queue."
   - Before: "They all stop at information, and information was never the problem. What's missing
     is somebody to do the work." After: "None of them add anyone who does the work."
2. **Short fragments as rhetorical beats.** "Ever." "By design." "Some teams do." "Every time, in
   every company." He writes under 1% of sentences at 5 words or fewer; a marketing page written by
   AI runs 5-10%. Fix: join the fragment to the sentence before it, or cut it.
   - Before: "No AI touches your production account. Ever." After: "No AI touches your production
     account"
   - Before: "They can. They mostly won't, and it isn't because they're lazy." After: "They can, but
     in practice they mostly don't, and it isn't laziness: they're measured on features, and cost
     tickets aren't features."
3. **Rule-of-three drumbeats and perfectly parallel clauses.** "Dashboards find the waste. Reports
   document it. Tickets get filed." / "assess, report, recommend, leave" / "$988,000 a year. Nine
   changes. One engineer." Fix: one sentence with "and", using the natural number of items.
   - After: "Dashboards find the waste and tickets get filed, but somebody still has to make the
     change." / "$988,000 a year from nine changes, made by one engineer"
4. **Portentous one-line summary closing a paragraph.** A short abstract sentence restating the
   concrete paragraph as a principle, often bold: "The arithmetic works for us, because we automated
   it." / "What you're buying is a lower bill next month." / "That is where most of the idle spend
   hides." Fix: cut it, or fold it into the paragraph as a plain clause ("For us the arithmetic
   works because we automated most of it").
5. **Rhetorical instructions and questions.** "Have a look at your ticket queue and count how
   many...", "Read that name again", "Picture a chip as...", "So why isn't everything on Spot?
   Because...". Fix: say the fact. "Most FinOps ticket queues have a few of these: unused RIs,
   oversized instances, orphaned volumes, filed months ago and still open."
6. **Throat-clearing before the point.** "Here's the part that doesn't make it into the survey.",
   "The reason is structural, and it's worth saying plainly:", "Below is what actually gets changed.
   It is deliberately specific, because...". Fix: delete the lead-in, start with the point.
7. **Semicolons joining clauses for balance, and any em-dash.** "A tool finds things; we implement
   them." becomes "A tool finds things, and we fix them". Use a comma, "and", "so", or a new
   sentence. Where a dash is unavoidable, a spaced hyphen " - " is what he types.
8. **Uncontracted forms.** "You have heard all of this before", "It is a work plan", "tooling I
   have been building", "engineering will not prioritize". Contract them all unless emphatic.
9. **Do-nothing adverbs.** actually, genuinely, precisely, simply, really, truly. "AWS cost
   optimization that actually gets implemented" becomes "AWS cost optimization that gets
   implemented". Keep "actually" only in his sense: "it actually works", "actually made".
10. **Words he never types.** battle-tested, robust, seamless, leverage, unlock, empower, delve,
    foundational, load-bearing, hand-wavy, "pattern recognition", "insights and learnings", "my
    craft". Replace with the plain thing: "battle-tested tooling" becomes "tooling built over 12
    years of doing this across many clients".
11. **Abstract metaphor instead of the mechanism.** "fighting structural incentives with force",
    "the end of the line and the front of a queue", "out the door", "the beachhead". Say what
    happens: "pushing engineers into work they aren't measured on".
12. **Claims with no number where he'd give one.** "helped thousands of companies save over
    $100,000,000" with no source; "typically costs less than a FinOps SaaS subscription" with no
    price on either side. Either attach the figure and its source or mark it `[NEEDS: ...]`.

## 3. Corporate-speak he rejected, and what he wrote instead

| Rejected | His replacement |
|---|---|
| implementation capacity | an engineer to do the work |
| delivered cost avoidance | savings on the bill |
| attribution window | a period we agree up front |
| sized to the footprint under our scope | with a lower percentage for larger customers |
| the shape of the wider engagement | how it works |
| context-dependent | dependent on the details of each account |
| AWS as our specialization | we specialize in AWS |
| visibility / surfacing spend | showing where the money goes |
| a consultancy / a practice | a FinOps implementation team (see below) |
| verified and specific | made and verified by hand |
| Seamless conversion | No downtime |
| right-configured Aurora | Aurora Serverless moved to provisioned |

## 4. Voice conventions for LeanerCloud copy

- **"We" is the default** for the company, the service, the process, pricing, security, procurement
  and the specialist network. "We make the changes ourselves", "we only ask for more access per
  change", "we'd rather confirm it with you than promise it".
- **Cristian is referred to in the third person, by name**, for credentials and history:
  "LeanerCloud is a FinOps implementation team, led by Cristian Magherusan-Stanciu, who has spent 12
  years in AWS cost optimization, some of them at AWS itself, in the EC2 team, as a Specialist
  Solutions Architect for Spot and Graviton." / "Cristian spent years waiting on engineering teams
  himself, so the whole approach was built around not having to."
- **"LeanerCloud"** is a name for the company and its products ("the LeanerCloud GUI", "our EBS
  Optimizer"), used sparingly. Never "at LeanerCloud we".
- **The company is "a FinOps implementation team".** Never "a consultancy" (the site argues against
  consultants who produce a PDF and leave) and never "a practice" (that word belongs to the
  customer's FinOps practice, which the service feeds rather than replaces).
- **Facts that carry a claim are not style choices.** "I run every engagement myself" or "nothing
  reaches production that I haven't checked" promise something; changing the pronoun changes the
  promise. Flag these instead of rewording them.
- **In his own posts and newsletters** (LinkedIn, blog under his byline) the voice is "I". He uses
  "we" only for the product and release voice ("we're happy to announce", "we now charge one month
  of savings per volume"), for the engagement process with a customer ("We usually start such
  engagements by going through the latest AWS bill"), and for himself plus the customer's team in a
  story. Never "we" for his own credentials or opinions.

## 5. Process rules

- **Never invent facts, numbers, client details or capabilities.** If a rewrite would read better
  with a specific detail you don't have, write `[NEEDS: what is missing]` and move on.
- **Preserve every figure, pricing term, spend threshold, security commitment and Marketplace/EDP
  wording exactly.** If a claim looks wrong or two pages contradict each other, put it in a separate
  "factual issues" list. Don't fix it silently inside a style rewrite.
- **Shorter is usually right.** If a paragraph survives at 60% of its length, cut it to 60%. When a
  sentence exists only for rhythm, delete it rather than rewording it.
- **Keep the persuasive structure.** The pages follow deliberate copywriting frameworks; fix the
  prose, don't reorder the argument.
- **Watch for over-correction.** A first de-AI pass pushed the site's mean sentence length past his
  own average and produced unreadable 60-word sentences that then had to be split again. Aim at his
  distribution (mean 25-30, plenty of 35-45 word sentences, almost nothing under 6 words), not at
  "longer". A sentence over ~50 words that isn't a list needs a second look.
- **Don't optimize against AI-detection tools.** They flag his real writing too. Re-read as the
  skeptical technical buyer instead.
- **US spelling** on the sites (optimization, anonymized). No em-dashes anywhere.

## 6. Verify the corpus before measuring against it

Before building or extending a fingerprint from a folder of posts, confirm each file is his:

```bash
git log --diff-filter=A --format='%h %ad %s' --date=short -- <file>
```

Anything added in a bulk SEO or "content" commit is almost certainly AI-written, and calibrating
against it produces a fingerprint of the AI. On leanercloud.com, 8 of the 14 `content/blog/*.md`
posts were added in two 2026-08-20 SEO commits (`d754656`, `45d0da6`) and have zero contractions in
600 words; they are excluded. The 5 posts imported in the 2026-07 theme migration (`4e37989`) carry
the original beehiiv footer and match the LinkedIn corpus, so they count.

The LinkedIn export is the primary source:
`/Users/cristi/Dropbox_Maestral/devel/linkedin-content-downloader/Basic_LinkedInDataExport_05-04-2025.zip/Articles/Articles/*.html`
(33 files, 6 of them empty drafts).

## 7. Measure a draft

`scripts/fingerprint.py` in this skill directory strips HTML or Markdown, prints contraction rate,
sentence-length distribution, banned words, connectives he never uses, and regex hits for the
structural tells, and marks each metric against the targets above:

```bash
python3 ~/.claude/skills/cristi-voice/scripts/fingerprint.py content/_index.md data/home_faq.json
```

The numbers are a smoke test, not the goal. A page can hit every target and still read as generated
if the antithesis and the drumbeats are still there, so do the checklist pass by hand as well.

## 8. Review checklist

Run on every paragraph before handing copy back:

1. Any "Not X. Y." / "isn't A, it's B" / "was never the problem" / "all of X and none of Y"? Flatten or cut.
2. Any sentence of 5 words or fewer that isn't a heading or a sign-off? Join or cut.
3. Three parallel clauses or a comma list arranged for rhythm? Rewrite with "and" and the real count.
4. A short abstract sentence closing the paragraph? Cut it.
5. An instruction or question aimed at the reader? Replace with the fact.
6. A lead-in before the point ("here's the part", "it's worth saying")? Delete it.
7. Semicolons between clauses, or any dash that isn't a spaced hyphen? Replace.
8. "You have", "it is", "does not", "cannot" where he'd contract? Contract.
9. actually / genuinely / precisely / simply / really doing no work? Delete.
10. battle-tested / robust / seamless / leverage / unlock / empower / delve / hand-wavy? Replace.
11. however / moreover / furthermore / that said / in fact / basically? Use and, but, so, also, then, still.
12. A claim with no number where he'd give one, or an unsourced statistic? Attach the figure or `[NEEDS: ...]`.
13. "I" where the site should say "we", or a pronoun change that alters a factual promise? Fix or flag.
14. "consultancy" or "practice" for LeanerCloud? Change to "FinOps implementation team".
15. Mean sentence length of the piece between 24 and 32, under 1% short sentences, nothing over ~50 words that isn't a list?
16. Read it once as a skeptical engineer. Anything that sounds like a slogan goes.
