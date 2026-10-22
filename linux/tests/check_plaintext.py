#!/usr/bin/env python3
"""Fails if any QML Text element can interpret markup.

Text defaults to AutoText, which renders HTML (including remote <img>) from strings such as other
people's display names. Every Text in Luma must set textFormat: Text.PlainText.
"""
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])
bad = []
for f in sorted(root.rglob("*.qml")):
    src = f.read_text()
    for m in re.finditer(r"\bText\s*\{", src):
        depth, i = 0, m.end() - 1
        while True:  # find the matching brace of this element
            if src[i] == "{":
                depth += 1
            elif src[i] == "}":
                depth -= 1
                if depth == 0:
                    break
            i += 1
        body = src[m.end():i]
        # only this element's own properties: drop nested child elements
        own = re.sub(r"\b[A-Z]\w*\s*\{[^{}]*\}", "", body)
        if "textFormat" not in own:
            bad.append(f"{f.relative_to(root)}:{src.count(chr(10), 0, m.start()) + 1}")
if bad:
    print("Text without textFormat: Text.PlainText:\n  " + "\n  ".join(bad))
    sys.exit(1)
print("all Text elements are plain text")
