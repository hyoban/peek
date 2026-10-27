# Makes icons/dev.png, the icon of Peek Dev, from resources/icon.png:
# the navy squircle turned orange, its shading and shadows kept.
#
#   uv run --with pillow --with numpy scripts/dev-icon.py resources/icon.png icons/dev.png

import sys
import numpy as np
from PIL import Image

src, dst = sys.argv[1], sys.argv[2]
im = np.asarray(Image.open(src).convert("RGBA")).astype(float)
h = im.shape[0]
rgb, a = im[..., :3], im[..., 3]
r, b = rgb[..., 0], rgb[..., 2]
lum = rgb @ np.array([0.299, 0.587, 0.114])
# The navy of each row, unshadowed: the brightest of its opaque navy.
navy = (b > r + 4) & (a > 250) & (lum < 90)
ref = np.array([np.percentile(lum[y][navy[y]], 90) if navy[y].sum() > 20 else np.nan for y in range(h)])
ys = np.arange(h)
ok = ~np.isnan(ref)
ref = np.interp(ys, ys[ok], ref[ok])
lo, hi = ref[ok].min(), ref[ok].max()
ref = ref[:, None]
mask = (b > r + 4) & (lum < ref * 1.25) & (a > 0)
# The orange of each row, lit as the navy was, shadows included.
t = ((ref - lo) / (hi - lo)).clip(0, 1)[..., None]
dark, light = np.array([194, 65, 12]), np.array([249, 140, 22])
col = (dark + (light - dark) * t) * (lum / ref)[..., None]
out = im.copy()
out[..., :3][mask] = col[mask]
Image.fromarray(out.clip(0, 255).astype(np.uint8)).save(dst)
