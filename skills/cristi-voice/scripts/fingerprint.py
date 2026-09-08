#!/usr/bin/env python3
"""Measure a draft against Cristian's voice fingerprint (see ../SKILL.md).

Usage: fingerprint.py FILE [FILE ...]   (HTML, Markdown, JSON or plain text; all files are pooled)
"""
import html
import json
import re
import statistics
import sys

TARGETS = {
    "contractions per 100 words": (1.4, 2.4),
    "mean words per sentence": (24, 32),
    "% sentences <= 5 words": (0, 1.5),
    "% sentences > 40 words": (8, 22),
    "% sentences > 50 words": (0, 8),
}

BANNED = ["genuinely", "precisely", "robust", "seamless", "leverage", "unlock", "empower", "delve",
          "foundational", "load-bearing", "battle-tested", "hand-wavy", "clearly", "obviously",
          "certainly", "absolutely", "truly", "extremely", "pattern recognition", "consultancy",
          "however", "moreover", "furthermore", "additionally", "that said", "in fact", "basically",
          "ultimately", "importantly", "notably", "in short", "in other words", "here's the thing",
          "the reality is", "it's worth saying", "it is worth noting", "worth saying"]

TELLS = {
    "antithesis (isn't A, it's B)": r"\b(isn't|aren't|wasn't|is not|are not|doesn't|don't)\b[^.;:!?]{2,60},\s*(it's|it is|they're|that's|what's missing|what)\b",
    "antithesis (Not X. / No X, no Y.)": r"(?:^|[.!?]\s+)(?:Not|No)\b[^.!?]{2,80}\.\s+(?:[A-Z]|$)",
    "'was never the problem'": r"\b(was|is) never the (problem|issue|point)\b",
    "'all of X and none of Y'": r"\ball (of )?the\b[^.]{2,40}\band none of\b",
    "portentous closer (That is where/why ...)": r"(?:^|\.\s+)That (is|'s) (where|why|what|how)\b[^.]{5,60}\.\s*$",
    "rhetorical instruction": r"(?:^|\.\s+)(Have a look|Picture|Imagine|Read that|Count how|Think about)\b",
    "semicolon between clauses": r"\w;\s+[a-z]",
    "em-dash or en-dash": r"[—–]",
    "uncontracted (it is / does not / cannot / you have)": r"\b(it is|that is|there is|do not|does not|did not|cannot|will not|is not|are not|you have|we have|I have|I am|you are|we are|they are)\b",
}


def strip_html(raw):
    raw = re.sub(r"<(script|style)[^>]*>.*?</\1>", " ", raw, flags=re.S | re.I)
    raw = re.sub(r"{{<.*?>}}", " ", raw, flags=re.S)
    raw = re.sub(r"<br\s*/?>|</p>|</li>|</h\d>|</div>|</td>", "\n", raw, flags=re.I)
    raw = re.sub(r"<[^>]+>", " ", raw)
    return html.unescape(raw)


def strip_md(raw):
    raw = re.sub(r"\A---.*?---", "", raw, flags=re.S)
    raw = re.sub(r"```.*?```", " ", raw, flags=re.S)
    raw = re.sub(r"!\[[^\]]*\]\([^)]*\)", " ", raw)
    raw = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", raw)
    raw = re.sub(r"^#+\s*", "", raw, flags=re.M)
    raw = re.sub(r"^\s*[-*]\s+", "", raw, flags=re.M)
    raw = re.sub(r"[*_`>|]", " ", raw)
    return raw


def json_strings(obj, out):
    if isinstance(obj, dict):
        for v in obj.values():
            json_strings(v, out)
    elif isinstance(obj, list):
        for v in obj:
            json_strings(v, out)
    elif isinstance(obj, str) and len(obj.split()) > 3:
        out.append(obj)


def load(path):
    raw = open(path, encoding="utf-8", errors="replace").read()
    if path.endswith(".json"):
        out = []
        json_strings(json.loads(raw), out)
        return "\n".join(out)
    if path.endswith((".html", ".htm")):
        return strip_html(raw)
    return strip_md(strip_html(raw))  # Hugo markdown mixes HTML in, so strip tags first


def sentences(text):
    text = re.sub(r"\s+", " ", text)
    text = re.sub(r"(\d)\.(\d)", r"\1<dot>\2", text)
    text = re.sub(r"\b(e\.g|i\.e|vs|etc)\.", r"\1<dot>", text)
    parts = re.split(r"(?<=[.!?])\s+(?=[A-Z\"'(\[$])", text)
    out = []
    for p in parts:
        p = p.replace("<dot>", ".").strip()
        n = len(re.findall(r"[A-Za-z0-9$'’%/.-]+", p))
        if n >= 2:
            out.append((n, p))
    return out


def status(name, value):
    lo, hi = TARGETS[name]
    return "ok " if lo <= value <= hi else "OUT"


def main(paths):
    text = "\n".join(load(p) for p in paths).replace("’", "'")
    words = re.findall(r"[A-Za-z0-9$']+", text)
    nw = len(words)
    if nw == 0:
        sys.exit("no text found")
    sents = sentences(text)
    lens = [n for n, _ in sents]
    contr = len(re.findall(r"\b\w+'(?:m|ll|ve|d|s|re|t)\b", text)) - len(re.findall(r"\b[A-Z]\w*'s\b", text))
    metrics = {
        "contractions per 100 words": 100 * contr / nw,
        "mean words per sentence": statistics.mean(lens),
        "% sentences <= 5 words": 100 * sum(1 for n, s in sents if n <= 5 and not s.endswith("?")) / len(lens),
        "% sentences > 40 words": 100 * sum(1 for n in lens if n > 40) / len(lens),
        "% sentences > 50 words": 100 * sum(1 for n in lens if n > 50) / len(lens),
    }
    print(f"{nw} words, {len(sents)} sentences, median {statistics.median(lens):.0f} words, "
          f"stdev {statistics.pstdev(lens):.1f}\n")
    for k, v in metrics.items():
        lo, hi = TARGETS[k]
        print(f"  {status(k, v)}  {k}: {v:.1f}   (target {lo}-{hi})")

    low = text.lower()
    hits = {w: len(re.findall(r"\b" + re.escape(w) + r"\b", low)) for w in BANNED}
    hits = {w: c for w, c in hits.items() if c}
    print("\nflagged words (never in his corpus, or against site positioning):", hits or "none")

    print("\nstructural tells (regex, check each by hand):")
    for name, rx in TELLS.items():
        found = [m.group(0).strip() for m in re.finditer(rx, text, flags=re.M)]
        if found:
            print(f"  {name}: {len(found)}")
            for f in found[:6]:
                print(f"      {f[:140]}")

    short = [s for n, s in sents if n <= 5 and not s.endswith("?")]  # FAQ questions are legitimately short
    if short:
        print("\nshort sentences:")
        for s in short[:15]:
            print(f"      {s}")
    long_ = sorted(((n, s) for n, s in sents if n > 50), reverse=True)
    if long_:
        print("\nsentences over 50 words (split unless it's a list):")
        for n, s in long_[:8]:
            print(f"  [{n}] {s[:160]}...")


if __name__ == "__main__":
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    main(sys.argv[1:])
