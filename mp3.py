"""
PDF -> MP3 + SRT captions + synced HTML reader (edge-tts),
with audio-friendly text cleanup and optional AI rewrite.

Setup (Windows):
    pip install -U edge-tts pypdf anthropic

Run (free cleanup only):
    python pdf_to_audiobook_v3.py "paper.pdf"

Run (AI rewrite, needs an Anthropic API key, costs a little):
    set ANTHROPIC_API_KEY=your_key_here
    python pdf_to_audiobook_v3.py "paper.pdf" --ai

Output (next to the PDF):
    paper.mp3   audio
    paper.srt   captions (works in VLC etc.)
    paper.html  open in browser: text highlights as it's read, shows the
                PDF page number, click any sentence to jump there
"""
import argparse
import asyncio
import html
import json
import re
from pathlib import Path

import edge_tts
from pypdf import PdfReader

BYTES_PER_SEC = 6000
AI_MODEL = "claude-sonnet-5-5"

REPLACE = [
    (r"\((?:[^()]*?\b(?:19|20)\d\d[a-z]?[^()]*?)\)", ""),   # (Glover, 1989)
    (r"\[\d+(?:[,\u2013-]\s*\d+)*\]", ""),                    # [12]
    (r"\bet al\.?", "and colleagues"),
    (r"\be\.g\.,?", "for example,"),
    (r"\bi\.e\.,?", "that is,"),
    (r"\bvs\.?", "versus"),
    (r"\bcf\.", "compare"),
    (r"\bSec\.", "Section"),
    (r"\u03bb", "lambda"), (r"\u03b1", "alpha"), (r"\u0394", "delta"),
    (r"\u2264", " is at most "), (r"\u2265", " is at least "),
    (r"\u2014|\u2013", ", "),
    (r"https?://\S+", ""),
    (r"\s+([,.;:])", r"\1"),
    (r"\s{2,}", " "),
]


def cleanText(t):
    for pat, rep in REPLACE:
        t = re.sub(pat, rep, t)
    return t.strip()


def aiRewrite(client, text):
    msg = client.messages.create(
        model=AI_MODEL,
        max_tokens=4000,
        messages=[{"role": "user", "content":
            "Rewrite this excerpt of an academic paper so it sounds natural "
            "when read aloud as an audiobook. Keep every idea and all "
            "technical content; do not add or invent anything. Use short, "
            "clear spoken sentences. Spell out symbols, formulas and "
            "abbreviations in words. Drop citations, footnote markers and "
            "figure references. If the excerpt is a code or algorithm box, "
            "describe it in plain words. Output plain text only: no "
            "markdown, no lists, no headings.\n\n" + text}],
    )
    return msg.content[0].text.strip()  # edge-tts default mp3: 48 kbps mono


def getPages(path, stopAtReferences):
    pages = []
    for page in PdfReader(path).pages:
        t = page.extract_text() or ""
        t = re.sub(r"(\w)-\n(\w)", r"\1\2", t)
        t = re.sub(r"\n\s*\d{1,3}\s*\n", "\n", t)
        t = re.sub(r"\s*\n\s*", " ", t)
        pages.append(cleanText(t))
    if stopAtReferences:
        for i, t in enumerate(pages):
            j = t.find(" References ")
            if j > 0:
                pages[i] = t[:j]
                pages = pages[: i + 1]
                break
    return pages


def chunkText(text, maxLen=3000):
    chunks, cur = [], ""
    for s in re.split(r"(?<=[.!?])\s+", text):
        if len(cur) + len(s) > maxLen and cur:
            chunks.append(cur.strip())
            cur = ""
        cur += s + " "
    if cur.strip():
        chunks.append(cur.strip())
    return chunks


def srtTime(t):
    ms = int(round(t * 1000))
    h, ms = divmod(ms, 3600000)
    m, ms = divmod(ms, 60000)
    s, ms = divmod(ms, 1000)
    return f"{h:02}:{m:02}:{s:02},{ms:03}"


async def synth(pages, mp3Path, voice, rate, client=None):
    items = []  # {start, end, text, page}
    offset = 0.0
    jobs = [(n + 1, c) for n, t in enumerate(pages) for c in chunkText(t)]
    with open(mp3Path, "wb") as f:
        for i, (pageNum, text) in enumerate(jobs, 1):
            print(f"Part {i}/{len(jobs)} (page {pageNum})")
            if client:
                text = aiRewrite(client, text)
            comm = edge_tts.Communicate(
                text, voice, rate=rate, boundary="SentenceBoundary"
            )
            audio = bytearray()
            events = []
            async for m in comm.stream():
                if m["type"] == "audio":
                    audio += m["data"]
                elif m["type"] == "SentenceBoundary":
                    events.append(m)
            f.write(audio)
            dur = len(audio) / BYTES_PER_SEC
            if not events:
                events = [{"offset": 0, "duration": dur * 1e7, "text": text}]
            for e in events:
                s = offset + e["offset"] / 1e7
                items.append(
                    {"start": s, "end": s + e["duration"] / 1e7,
                     "text": e["text"], "page": pageNum}
                )
            offset += dur
    return items


def writeSrt(items, path):
    with open(path, "w", encoding="utf-8") as f:
        for i, it in enumerate(items, 1):
            f.write(f"{i}\n{srtTime(it['start'])} --> {srtTime(it['end'])}\n"
                    f"{it['text']}\n\n")


PAGE = """<!doctype html><meta charset="utf-8"><title>__TITLE__</title>
<style>
body{font:18px/1.7 Georgia,serif;max-width:800px;margin:0 auto;padding:90px 20px 40px}
#bar{position:fixed;top:0;left:0;right:0;background:#fff;border-bottom:1px solid #ccc;
padding:8px 16px;z-index:5}
#bar audio{width:75%;vertical-align:middle}#pg{margin-left:12px;font:600 15px sans-serif}
.s{cursor:pointer}.s:hover{background:#eef}.on{background:#ffe066}
.pn{display:block;margin-top:26px;font:700 13px sans-serif;color:#888}
</style>
<div id="bar"><audio id="a" controls src="__MP3__"></audio><span id="pg"></span></div>
<div id="t"></div>
<script>
const D=__DATA__,a=document.getElementById('a'),t=document.getElementById('t'),
pg=document.getElementById('pg');let last=0,cur=-1,spans=[];
D.forEach((d,i)=>{
 if(i==0||D[i-1].page!=d.page){const p=document.createElement('span');
  p.className='pn';p.textContent='PDF page '+d.page;t.appendChild(p);}
 const s=document.createElement('span');s.className='s';s.textContent=d.text+' ';
 s.onclick=()=>{a.currentTime=d.start;a.play();};t.appendChild(s);spans.push(s);});
a.ontimeupdate=()=>{const x=a.currentTime;let i=cur;
 if(i<0||x<D[i].start||x>=D[i].end){i=D.findIndex(d=>x>=d.start&&x<d.end);}
 if(i>=0&&i!=cur){if(cur>=0)spans[cur].classList.remove('on');
  spans[i].classList.add('on');spans[i].scrollIntoView({block:'center',behavior:'smooth'});
  pg.textContent='Page '+D[i].page+' of '+D[D.length-1].page;cur=i;}};
</script>"""


def writeHtml(items, path, title, mp3Name):
    page = (PAGE.replace("__TITLE__", html.escape(title))
            .replace("__MP3__", mp3Name)
            .replace("__DATA__", json.dumps(items)))
    Path(path).write_text(page, encoding="utf-8")


def main():
    p = argparse.ArgumentParser()
    p.add_argument("pdf")
    p.add_argument("--voice", default="en-US-AriaNeural")
    p.add_argument("--rate", default="+0%")
    p.add_argument("--keep-references", action="store_true")
    p.add_argument("--ai", action="store_true", help="AI rewrite for listening")
    a = p.parse_args()

    base = Path(a.pdf).with_suffix("")
    pages = getPages(a.pdf, not a.keep_references)
    client = None
    if a.ai:
        import anthropic
        client = anthropic.Anthropic()  # reads ANTHROPIC_API_KEY
    items = asyncio.run(
        synth(pages, str(base) + ".mp3", a.voice, a.rate, client))
    writeSrt(items, str(base) + ".srt")
    writeHtml(items, str(base) + ".html", base.name, base.name + ".mp3")
    print("Done. Open", str(base) + ".html")


if __name__ == "__main__":
    main()