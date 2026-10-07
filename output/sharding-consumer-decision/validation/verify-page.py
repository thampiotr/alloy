#!/usr/bin/env python3
"""Validate HTML structure and exercise controls without a browser or network."""
from collections import Counter
from html.parser import HTMLParser
from pathlib import Path
import json
import subprocess
import tempfile


class Check(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.ids, self.controls, self.assets, self.stack, self.nodes = [], [], [], [], []

    def handle_starttag(self, tag, attrs):
        a = dict(attrs)
        if "id" in a:
            self.ids.append(a["id"])
        if "aria-controls" in a:
            self.controls.append(a["aria-controls"])
        if tag in {"script", "img", "link"} and ("src" in a or "href" in a):
            self.assets.append(a)
        if any(k in {"id", "role"} or k.startswith("data-") for k in a):
            self.nodes.append({"tag": tag, "attrs": a})
        if tag not in {"meta", "link", "br", "hr", "img", "input", "source", "wbr"}:
            self.stack.append(tag)

    def handle_endtag(self, tag):
        assert self.stack and self.stack[-1] == tag, (tag, self.stack[-4:])
        self.stack.pop()


here = Path(__file__).resolve().parent
page = here.parent.parent / "loki-pipeline-research.html"
html = page.read_text()
check = Check()
check.feed(html)
assert not check.stack
assert not [k for k, v in Counter(check.ids).items() if v > 1]
assert all(x in check.ids for x in check.controls)
assert not check.assets
assert len([x for x in check.ids if x.startswith("task-")]) == 6
script = html.split("<script>", 1)[1].split("</script>", 1)[0]
with tempfile.TemporaryDirectory(prefix="alloy-page-check-") as temporary:
    temporary = Path(temporary)
    js, elements = temporary / "page.js", temporary / "elements.json"
    js.write_text(script)
    elements.write_text(json.dumps(check.nodes))
    subprocess.run(["node", "--check", str(js)], check=True)
    subprocess.run(["node", str(here / "check-page.cjs"), str(elements), str(js)], check=True)
print("PASS: HTML nesting, unique IDs, six task panels, navigation references, no external assets.")
