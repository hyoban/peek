#!/bin/sh
# Rebuilds fileicons from the latest file-icons and Octicons 4.4.0.
# Needs git, npm, node and Python's fontTools (pip install fonttools brotli).
set -eu
cd "$(dirname "$0")/../.."
work=$(mktemp -d)
git clone -q --depth 1 https://github.com/file-icons/atom.git "$work/atom"
(cd "$work" && npm pack -q octicons@4.4.0 >/dev/null && tar -xzf octicons-4.4.0.tgz)
node fileicons/gen/dump.js "$work/atom/lib/icons/.icondb.js" > "$work/icondb.json"
python3 - "$work/atom/fonts" fileicons/fonts <<'PY'
import sys, pathlib
from fontTools.ttLib import TTFont
src, dst = map(pathlib.Path, sys.argv[1:])
for woff2 in src.glob("*.woff2"):
    font = TTFont(woff2)
    font.flavor = None
    font.save(dst / (woff2.stem + ".ttf"))
PY
cp "$work/package/build/font/octicons.ttf" fileicons/fonts/
cp "$work/atom/LICENSE.md" fileicons/LICENSE-file-icons.md
go run ./fileicons/gen "$work/icondb.json" "$work/atom/styles/icons.less" "$work/package/build/font/octicons.css"
rm -rf "$work"
