#!/usr/bin/env python3
"""Count lines of code in the repository, by language, with and without tests.

Counts only files git tracks. A line counts when something other than a comment
or whitespace is left on it; docs, data and lockfiles (Markdown, JSON, YAML,
CSV, SVG, ...) are not code and are not counted at all.

Comment stripping is lexical, not a parser: a comment marker inside a string
literal is taken for a comment. Close enough for a headcount.

Usage: scripts/loc.py
"""

import re
import subprocess
import sys
from collections import defaultdict
from pathlib import Path

# extension -> (language, line-comment markers, block-comment delimiters)
C_STYLE = (["//"], [("/*", "*/")])
LANGUAGES = {
    ".go": ("Go", *C_STYLE),
    ".ts": ("TypeScript", *C_STYLE),
    ".tsx": ("TypeScript", *C_STYLE),
    ".js": ("JavaScript", *C_STYLE),
    ".css": ("CSS", [], [("/*", "*/")]),
    ".py": ("Python", ["#"], [('"""', '"""'), ("'''", "'''")]),
    ".sh": ("Shell", ["#"], []),
    ".sql": ("SQL", ["--"], [("/*", "*/")]),
    ".html": ("HTML", [], [("<!--", "-->")]),
}

TEST_PATTERNS = [
    re.compile(p)
    for p in (
        r"_test\.go$",
        r"\.test\.tsx?$",
        r"(^|/)test_[^/]*\.py$",
        r"(^|/)conftest\.py$",
        r"(^|/)tests?/",
        r"(^|/)[^/]*test/",  # dbtest/ and the like
    )
]


def is_test(path):
    return any(p.search(path) for p in TEST_PATTERNS)


def count_code_lines(text, line_markers, blocks):
    """Lines left with something other than whitespace once comments are gone."""
    count = 0
    open_block = None  # closing delimiter of the block comment we are inside
    for line in text.splitlines():
        rest, code = line, ""
        while rest:
            if open_block:
                end = rest.find(open_block)
                if end < 0:
                    rest = ""
                else:
                    rest, open_block = rest[end + len(open_block):], None
                continue
            # Earliest comment opener on what is left of the line.
            hits = [(rest.find(m), m, None) for m in line_markers]
            hits += [(rest.find(s), s, e) for s, e in blocks]
            hits = [h for h in hits if h[0] >= 0]
            if not hits:
                code += rest
                break
            pos, opener, closer = min(hits, key=lambda h: h[0])
            # Python triple quotes are only a "comment" when they open the line
            # (a docstring); elsewhere they are an ordinary string.
            if opener in ('"""', "'''") and (code + rest[:pos]).strip():
                code += rest[: pos + 3]
                rest = rest[pos + 3:]
                continue
            code += rest[:pos]
            if closer is None:
                break
            rest, open_block = rest[pos + len(opener):], closer
        if code.strip():
            count += 1
    return count


def main():
    root = Path(
        subprocess.check_output(["git", "rev-parse", "--show-toplevel"], text=True).strip()
    )
    files = subprocess.check_output(["git", "ls-files", "-z"], cwd=root, text=True).split("\0")

    totals = defaultdict(lambda: [0, 0, 0])  # language -> [files, code, test]
    for path in filter(None, files):
        spec = LANGUAGES.get(Path(path).suffix)
        full = root / path
        if not spec or not full.is_file():
            continue
        lang, line_markers, blocks = spec
        text = full.read_text(encoding="utf-8", errors="replace")
        n = count_code_lines(text, line_markers, blocks)
        row = totals[lang]
        row[0] += 1
        row[2 if is_test(path) else 1] += n

    header = f"{'Language':<12} {'Files':>6} {'Non-test':>9} {'Test':>8} {'Total':>8}"
    print(header)
    print("-" * len(header))
    grand = [0, 0, 0]
    for lang, (f, code, test) in sorted(totals.items(), key=lambda kv: -(kv[1][1] + kv[1][2])):
        print(f"{lang:<12} {f:>6} {code:>9} {test:>8} {code + test:>8}")
        grand = [grand[0] + f, grand[1] + code, grand[2] + test]
    print("-" * len(header))
    f, code, test = grand
    print(f"{'Total':<12} {f:>6} {code:>9} {test:>8} {code + test:>8}")


if __name__ == "__main__":
    sys.exit(main())
