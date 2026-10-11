#!/usr/bin/env python3
"""Build the data for the Kyōtsu trainer's Tokyo (標準語) mode from the Kotu minimal pairs.

The pairs and recordings are Kotu's (kotu.io, by k3zi), from the NHK accent dictionary, as
kept by https://github.com/Kuuuube/minimal-pairs. The recordings are NHK's, so the output is
NEVER committed or put in static/: it goes to the server's data directory and fazond serves
it only to accounts with Tokyo-mode access (api/hyojungo.go).

    git clone --depth 1 https://github.com/Kuuuube/minimal-pairs.git /tmp/kmp
    python3 backend/deploy/hyojungo_build.py /tmp/kmp /tmp/hyojungo
    rsync -a --delete /tmp/hyojungo/ root@HOST:/var/lib/fazond/hyojungo/
    ssh root@HOST chown -R fazond: /var/lib/fazond/hyojungo

Output:
    index.json        {"pairs":[{"id","kana","m":[{"pron","acc","pat","cat","dv","a"}]}]}
    audio/<a>.aac     one ADTS AAC clip per member, <a> = "<pair id>-<member index>"

pat is the trainer's H/L pattern (↘ = a drop after the last mora), cat Kotu's stats bucket
(TH = Heiban / Odaka, TA = Atamadaka, TN = Nakadaka), dv the 0-based devoiced morae.
"""
import base64
import json
import os
import shutil
import sys

# kana that join the previous one into a single mora (゚ = the nasal-g mark, e.g. カ゚)
JOIN = set("ャュョァィゥェォヮ゚")


def moras(s):
    out = []
    for c in s:
        if c in JOIN and out:
            out[-1] += c
        else:
            out.append(c)
    return out


def pattern(acc, n):
    """Tokyo accent number → H/L per mora, ↘ marking a drop after the last mora."""
    if acc == 0:
        return "H" if n == 1 else "L" + "H" * (n - 1)
    if acc == 1:
        return "H↘" if n == 1 else "H" + "L" * (n - 1)
    if acc == n:
        return "L" + "H" * (n - 1) + "↘"
    return "L" + "H" * (acc - 1) + "L" * (n - acc)


def category(acc, n):
    # Kotu's own buckets (Tests/PitchAccent/MinimalPairs.js): odaka counts with heiban
    if acc == 0 or acc == n:
        return "TH"
    return "TA" if acc == 1 else "TN"


def main(src, out):
    data = os.path.join(src, "data")
    if os.path.exists(out):
        shutil.rmtree(out)
    os.makedirs(os.path.join(out, "audio"))
    pairs, clips, size = [], 0, 0
    for pid in sorted(os.listdir(data), key=lambda s: (len(s), s)):
        with open(os.path.join(data, pid), encoding="utf-8") as f:
            d = json.load(f)
        members = []
        for i, p in enumerate(d["pairs"]):
            pron, acc, n = p["rawPronunciation"], p["pitchAccent"], p["moraCount"]
            if len(moras(pron)) != n:
                sys.exit(f"{pid}: {pron} has {len(moras(pron))} morae, data says {n}")
            if not 0 <= acc <= n:
                sys.exit(f"{pid}: {pron} accent {acc} out of range")
            a = f"{pid}-{i}"
            audio = base64.b64decode(p["soundData"])
            if audio[:2] != b"\xff\xf9" and audio[:2] != b"\xff\xf1":
                sys.exit(f"{pid}: member {i} isn't ADTS AAC")
            with open(os.path.join(out, "audio", a + ".aac"), "wb") as f:
                f.write(audio)
            clips += 1
            size += len(audio)
            members.append({"pron": pron, "acc": acc, "pat": pattern(acc, n), "cat": category(acc, n),
                            "dv": sorted(s - 1 for s in p["silencedMoras"]), "a": a})
        if len({m["pat"] for m in members}) != len(members):
            print(f"skipping {pid} ({d['kana']}): two members share a pattern", file=sys.stderr)
            continue
        pairs.append({"id": pid, "kana": d["kana"], "m": members})
    with open(os.path.join(out, "index.json"), "w", encoding="utf-8") as f:
        json.dump({"pairs": pairs}, f, ensure_ascii=False, separators=(",", ":"))
    print(f"{len(pairs)} pairs, {clips} clips, {size / 1e6:.1f} MB audio → {out}")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    main(sys.argv[1], sys.argv[2])
