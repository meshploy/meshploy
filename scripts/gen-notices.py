#!/usr/bin/env python3
"""Generate the third-party notices that ship with each Meshploy artifact.

MIT, BSD, ISC and Apache-2.0 all require their copyright lines and licence
text to travel with binaries, so every image and release asset carries the
notices for what it bundles.

The list comes from the artifact itself, never from a hand-kept file: for a Go
binary it is the module list the toolchain records inside it, so only what was
actually linked is named, and for the console it is the production dependency
tree, so build tooling is left out.

  python3 scripts/gen-notices.py           regenerate notices/
  python3 scripts/gen-notices.py --check   fail if the committed files are stale

Run `npm ci` in apps/web first. The console's list comes from what is installed,
so a node_modules that has drifted from the lock file produces a file that names
versions nobody ships, and the check in CI, which installs from the lock, then
disagrees with it.

The check is what CI runs, the way it checks formatting.
"""

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
OUT_DIR = ROOT / "notices"
# The console bundle is served from the web image, which ships this directory.
WEB_NOTICES = ROOT / "apps" / "web" / "public" / "third-party-notices.md"

# Go binaries, by the name their notices file carries.
GO_BINARIES = {"api": "./apps/api", "proxy": "./apps/proxy", "cli": "./apps/cli"}

LICENSE_FILE = re.compile(r"^(licen[sc]e|copying|unlicense|notice)([.-].*)?$", re.I)

# Enough to tell the families apart for the deny gate below. It reports what a
# text says about itself; a text it cannot place is listed as unknown rather
# than guessed at.
# Titles, matched against the head of the text only. Searching the whole body
# put every MPL-2.0 module in the GPL bucket, since MPL section 3.3 names the
# GNU licences.
TITLES = [
    ("AGPL-3.0", "gnu affero general public license"),
    ("SSPL", "server side public license"),
    ("LGPL", "gnu lesser general public license"),
    ("GPL", "gnu general public license"),
    ("MPL-2.0", "mozilla public license"),
    ("Apache-2.0", "apache license"),
    ("OFL-1.1", "sil open font license"),
]

# Bodies, for the licences that state their terms without a title.
BODIES = [
    ("BSD", "redistribution and use in source and binary forms"),
    ("ISC", "permission to use, copy, modify, and/or distribute"),
    ("MIT", "permission is hereby granted, free of charge"),
    ("Unlicense", "this is free and unencumbered software released into the public domain"),
]

HEAD = 600  # characters of title region

# A binary may not carry these. Copyleft that reaches a whole work does not
# belong in a product shipped under Apache-2.0 and a commercial edition.
DENIED = {"AGPL-3.0", "GPL", "SSPL"}


def run(cmd, cwd=None, check=True):
    p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True)
    if check and p.returncode != 0:
        sys.exit(f"{' '.join(cmd)} failed:\n{p.stderr.strip()}")
    return p.stdout


def classify(text, declared=""):
    low = text.lower()
    for name, marker in TITLES:
        if marker in low[:HEAD]:
            return name
    for name, marker in BODIES:
        if marker in low:
            return name
    for name, marker in TITLES:
        if marker in low:
            return name
    # Nothing recognisable in the text: what the package says about itself, or
    # unknown, which reads as "a human has to look at this one".
    return declared or "Unknown"


def escape_module(path):
    """A module path as the module cache spells it: Foo becomes !foo."""
    return re.sub(r"[A-Z]", lambda m: "!" + m.group(0).lower(), path)


def find_license_text(directory):
    if not directory.is_dir():
        return None
    candidates = []
    for depth, base in ((0, directory), (1, directory)):
        for entry in sorted(base.rglob("*") if depth else base.iterdir()):
            if depth and len(entry.relative_to(base).parts) > 2:
                continue
            if entry.is_file() and LICENSE_FILE.match(entry.name):
                candidates.append(entry)
        if candidates:
            break
    if not candidates:
        return None
    # A licence beside the module root beats one nested in an example directory.
    candidates.sort(key=lambda p: (len(p.relative_to(directory).parts), p.name.lower()))
    return candidates[0].read_text(errors="replace").strip()


def go_modules(binary, gomodcache):
    """The modules recorded inside a built binary, with their licence text."""
    out = run(["go", "version", "-m", str(binary)])
    found, missing = [], []
    seen = set()
    for line in out.splitlines():
        parts = line.split("\t")
        if len(parts) < 3 or parts[1] not in ("dep", "=>"):
            continue
        path, version = parts[2], parts[3] if len(parts) > 3 else ""
        if not version.startswith("v") or (path, version) in seen:
            continue
        seen.add((path, version))
        text = find_license_text(Path(gomodcache) / f"{escape_module(path)}@{version}")
        (found if text else missing).append((path, version, text, ""))
    return found, missing


# Packages whose files reach the browser as assets rather than as code, so no
# sourcemap names them. Fonts are the whole list today.
ASSET_PACKAGES = ["geist"]
BUNDLED_FROM_NODE_MODULES = re.compile(r"node_modules/((?:@[^/]+/)?[^/]+)/")


def web_packages(app_dir):
    """The packages whose code is in the console bundle the image serves.

    The web image ships the built assets and nothing else, so the dependency
    tree would over-report: half of it is build tooling that never reaches a
    browser. A production build with sourcemaps names exactly what went in.
    """
    found, missing = [], []
    with tempfile.TemporaryDirectory() as out:
        run(["npx", "vite", "build", "--sourcemap", "true", "--outDir", out, "--emptyOutDir"], cwd=app_dir)
        names = set()
        for sourcemap in Path(out).rglob("*.map"):
            data = json.loads(sourcemap.read_text(errors="replace"))
            for source in data.get("sources") or []:
                m = BUNDLED_FROM_NODE_MODULES.search(source)
                if m:
                    names.add(m.group(1))
        fonts = any(Path(out).rglob(f"*{ext}") for ext in (".woff2", ".woff", ".ttf", ".otf"))
        if fonts:
            names.update(n for n in ASSET_PACKAGES if (app_dir / "node_modules" / n).is_dir())

    for name in sorted(names):
        path = app_dir / "node_modules" / name
        if not path.is_dir():
            missing.append((name, "", None, ""))
            continue
        version, declared = "", ""
        pkg = path / "package.json"
        if pkg.is_file():
            meta = json.loads(pkg.read_text(errors="replace"))
            version = meta.get("version", "")
            lic = meta.get("license")
            declared = lic if isinstance(lic, str) else (lic or {}).get("type", "")
        text = find_license_text(path)
        if not text and declared:
            text = f"{name} ships no licence file. Its package.json declares: {declared}."
        (found if text else missing).append((name, version, text, declared))
    return found, missing


def render(artifact, entries):
    """One section per distinct licence text, with the components under it.

    Grouping by the text keeps every copyright line intact, since a different
    holder means a different text and so a section of its own.
    """
    groups = {}
    for name, version, text, declared in entries:
        key = hashlib.sha256(text.encode()).hexdigest()
        groups.setdefault(key, {"text": text, "license": classify(text, declared), "parts": []})
        groups[key]["parts"].append(f"{name} {version}".strip())

    order = sorted(groups.values(), key=lambda g: (g["license"], g["parts"][0].lower()))
    lines = [
        f"# Third-party notices: Meshploy {artifact}",
        "",
        "Generated by `scripts/gen-notices.py`. Do not edit by hand.",
        "",
        f"This artifact bundles {len(entries)} components. Each is used under its own",
        "licence, reproduced below. Meshploy's own code is licensed separately: see",
        "the LICENSE file that ships beside this one.",
        "",
        "## Summary",
        "",
        "| Licence | Components |",
        "|---|---|",
    ]
    tally = {}
    for g in order:
        tally[g["license"]] = tally.get(g["license"], 0) + len(g["parts"])
    for lic, count in sorted(tally.items(), key=lambda kv: (-kv[1], kv[0])):
        lines.append(f"| {lic} | {count} |")
    lines.append("")

    for g in order:
        lines.append(f"## {g['license']}")
        lines.append("")
        for part in sorted(g["parts"], key=str.lower):
            lines.append(f"- {part}")
        lines.append("")
        lines.append("```")
        lines.append(g["text"])
        lines.append("```")
        lines.append("")
    return "\n".join(lines).rstrip() + "\n"


def generate():
    """Return {destination path: file content} for every artifact."""
    files, problems, denied = {}, [], []
    gomodcache = run(["go", "env", "GOMODCACHE"], cwd=ROOT).strip()
    goroot = run(["go", "env", "GOROOT"], cwd=ROOT).strip()
    go_license = (Path(goroot) / "LICENSE").read_text(errors="replace").strip()
    goversion = run(["go", "env", "GOVERSION"], cwd=ROOT).strip()

    with tempfile.TemporaryDirectory() as tmp:
        for artifact, pkg in GO_BINARIES.items():
            binary = Path(tmp) / artifact
            run(["go", "build", "-o", str(binary), pkg], cwd=ROOT)
            found, missing = go_modules(binary, gomodcache)
            found.append(("The Go standard library", goversion, go_license, ""))
            problems += [f"{artifact}: no licence file for {n} {v}" for n, v, _, _ in missing]
            denied += [f"{artifact}: {n} {v} is {classify(t, d)}" for n, v, t, d in found if classify(t, d) in DENIED]
            files[OUT_DIR / f"THIRD-PARTY-NOTICES-{artifact}.md"] = render(artifact, found)

    web = ROOT / "apps" / "web"
    if (web / "node_modules").is_dir():
        found, missing = web_packages(web)
        problems += [f"console: no licence for {n} {v}" for n, v, _, _ in missing]
        denied += [f"console: {n} {v} is {classify(t, d)}" for n, v, t, d in found if classify(t, d) in DENIED]
        # Served by the web image, which ships apps/web/public with the bundle.
        files[WEB_NOTICES] = render("console", found)
    else:
        problems.append("console: node_modules is missing, run npm ci in apps/web first")

    return files, problems, denied


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true", help="fail when the committed notices are stale")
    args = ap.parse_args()

    files, problems, denied = generate()

    for line in denied:
        print(f"DENIED LICENCE: {line}", file=sys.stderr)
    for line in problems:
        print(f"warning: {line}", file=sys.stderr)

    status = 0
    if denied:
        print("\nA shipped artifact may not bundle AGPL, GPL or SSPL code.", file=sys.stderr)
        status = 1

    if args.check:
        for path, content in sorted(files.items()):
            if not path.is_file() or path.read_text() != content:
                print(f"{path.relative_to(ROOT)} is out of date. Run scripts/gen-notices.py.", file=sys.stderr)
                status = 1
        if status == 0:
            print(f"{len(files)} notices files are up to date")
    else:
        for path, content in sorted(files.items()):
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
            print(f"wrote {path.relative_to(ROOT)}")

    sys.exit(status)


if __name__ == "__main__":
    main()
