#!/usr/bin/env python3
"""
Parse nendai_ryuukou_by_year_EN.md into an embedded JS data file for the
/tools/nendai/ birth-year tool. Output: static/nendai/nendai_data.js
(assigns window.NENDAI_DATA). Re-run after editing the source markdown.
"""
import json, re, sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from nendai_supplements import ADULT_ANIME, TOKU_BY_YEAR, GAMES_BY_YEAR

SRC = Path("/home/faizan/Language Learning/nendai_ryuukou_by_year_EN.md")
OUT = Path(__file__).parent / "static" / "nendai" / "nendai_data.js"

# H3 Japanese code (inside parens) -> (bucket key, kind)
CAT = {
    "出来事": ("events", "bullet"),
    "流行ファッション・髪型": ("fashion", "bullet"),
    "新商品・ヒット商品": ("products", "bullet"),
    "新食品・ヒット食品": ("foods", "bullet"),
    "流行語": ("buzzwords", "bullet"),
    "ベストセラー": ("bestsellers", "bullet"),
    "流行・出来事": ("trends", "bullet"),
    "ヒット曲": ("songs", "table"),
    "邦楽ヒット曲": ("songs", "table"),
    "邦画ランキング": ("jpfilms", "table"),
    "洋画ランキング": ("foreignfilms", "table"),
    "名作アニメ": ("anime", "table"),
    "アニメ": ("anime", "table"),
    "高視聴率テレビドラマ": ("dramas", "table"),
    "ドラマ視聴率": ("dramas", "table"),
}

YEAR_RE = re.compile(r"^## (\d{4}) \((.+)\)\s*$")
CAT_RE = re.compile(r"^### .*?\((.+?)\)\s*$")


def strip_bold(s):
    return s.replace("**", "").strip()


def strip_md(s):
    """For bullet items: drop bold and italic markers (safe — no literal * here)."""
    return s.replace("**", "").replace("*", "").strip()


def norm_table_row(bucket, cols, cells):
    """Map a table row (header->value) to {rank?, label, sub}."""
    d = {c.lower(): v for c, v in zip(cols, cells)}
    rank = d.get("rank")
    if bucket == "songs":
        return {"rank": rank, "label": d.get("song", ""), "sub": d.get("artist", "")}
    if bucket in ("jpfilms", "foreignfilms"):
        title = d.get("title") or d.get("title (original)") or ""
        return {"rank": rank, "label": title, "sub": d.get("box office", "")}
    if bucket == "anime":
        row = {"label": d.get("title", ""), "sub": d.get("studio", "")}
        # 2010's table spans the whole 2010s via a "Year" column; honor it.
        yr = d.get("year", "")
        if re.fullmatch(r"\d{4}", yr):
            row["_year"] = yr
        return row
    if bucket == "dramas":
        sub = d.get("peak rating", "")
        if d.get("station"):
            sub = f"{sub} · {d['station']}" if sub else d["station"]
        return {"rank": rank, "label": d.get("drama", ""), "sub": sub}
    # fallback: first non-rank column as label
    vals = [v for c, v in zip(cols, cells) if c.lower() != "rank"]
    return {"rank": rank, "label": vals[0] if vals else "", "sub": " · ".join(vals[1:])}


def main():
    lines = SRC.read_text(encoding="utf-8").splitlines()
    years = {}
    cur_year = None
    cur_bucket = None
    cur_kind = None
    tbl_cols = None  # None until header row seen; [] after
    for raw in lines:
        line = raw.rstrip()
        m = YEAR_RE.match(line)
        if m:
            cur_year = m.group(1)
            years[cur_year] = {"era": m.group(2), "buckets": {}}
            cur_bucket = cur_kind = None
            tbl_cols = None
            continue
        m = CAT_RE.match(line)
        if m and cur_year:
            code = m.group(1)
            info = CAT.get(code)
            if info:
                cur_bucket, cur_kind = info
                years[cur_year]["buckets"].setdefault(cur_bucket, [])
                tbl_cols = None
            else:
                cur_bucket = cur_kind = None
            continue
        if line.startswith("## ") or line.startswith("# "):
            cur_bucket = cur_kind = None
            continue
        if not cur_year or not cur_bucket:
            continue
        bucket = years[cur_year]["buckets"][cur_bucket]
        if cur_kind == "bullet":
            if line.startswith("- "):
                item = line[2:].strip()
                if cur_bucket == "buzzwords":
                    if " — " in item:
                        term, note = item.split(" — ", 1)
                        bucket.append({"label": strip_md(term), "sub": strip_md(note)})
                    else:
                        bucket.append({"label": strip_md(item), "sub": ""})
                else:
                    bucket.append({"label": strip_md(item), "sub": ""})
        elif cur_kind == "table":
            if line.startswith("|"):
                cells = [c.strip() for c in line.strip().strip("|").split("|")]
                if all(set(c) <= set("-: ") for c in cells):
                    continue  # separator row
                if tbl_cols is None:
                    tbl_cols = cells  # header
                else:
                    bucket.append(norm_table_row(cur_bucket, tbl_cols, cells))

    # relocate anime rows that carried their own "Year" column (the 2010 decade table)
    for y in list(years):
        anime = years[y]["buckets"].get("anime")
        if not anime:
            continue
        keep = []
        for it in anime:
            tgt = it.pop("_year", None)
            if tgt and tgt != y and tgt in years:
                years[tgt]["buckets"].setdefault("anime", []).append(it)
            else:
                keep.append(it)
        years[y]["buckets"]["anime"] = keep

    # tag anime demographic tier: "adult" (seinen/josei — gated to teens) vs "youth"
    unmatched = []
    for y in years.values():
        for it in y["buckets"].get("anime", []):
            eng = it["label"].split(" (")[0].strip()
            it["demo"] = "adult" if eng in ADULT_ANIME else "youth"

    # inject curated tokusatsu + video-game buckets (source has neither)
    for yr, rows in TOKU_BY_YEAR.items():
        if yr in years:
            years[yr]["buckets"]["toku"] = [dict(r, demo="youth") for r in rows]
    for yr, rows in GAMES_BY_YEAR.items():
        if yr in years:
            years[yr]["buckets"]["games"] = [dict(r) for r in rows]

    # prune empty buckets
    for y in years.values():
        y["buckets"] = {k: v for k, v in y["buckets"].items() if v}

    yr_keys = sorted(int(y) for y in years)
    meta = {"minYear": yr_keys[0], "maxYear": yr_keys[-1], "count": len(yr_keys)}
    OUT.parent.mkdir(parents=True, exist_ok=True)
    payload = {"meta": meta, "years": years}
    js = "window.NENDAI_DATA = " + json.dumps(payload, ensure_ascii=False, separators=(",", ":")) + ";\n"
    OUT.write_text(js, encoding="utf-8")
    print(f"Wrote {OUT} ({OUT.stat().st_size:,} bytes) — {meta}")


if __name__ == "__main__":
    main()
