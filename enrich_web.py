#!/usr/bin/env python3
"""Enrich leads by scraping their own websites for emails and mobile numbers.
Only works for leads whose site resolves (WebStatus live/exists) - i.e. NOT the
no-site hot leads. Stdlib only (urllib), polite concurrency, short timeouts."""
import csv, re, socket, urllib.request, urllib.parse
from concurrent.futures import ThreadPoolExecutor

socket.setdefaulttimeout(8)
UA = "HostoLeadGen/1.0 (+mailto:rosh.s568@gmail.com)"

EMAIL = re.compile(r"[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}")
MOBILE = re.compile(r"(?<!\d)(?:\+?91[\-\s]?|0)?([6-9]\d{9})(?!\d)")
BAD = ("example.", "sentry", "wix", "godaddy", "domain.com", "email.com",
       "yourname", "your-email", "@2x", ".png", ".jpg", ".jpeg", ".gif", ".webp",
       "u003e", "react", "schema.org", "sentry.io", "wordpress")


def fetch(url):
    try:
        req = urllib.request.Request(url, headers={"User-Agent": UA})
        with urllib.request.urlopen(req) as r:
            return r.read(600_000).decode("utf-8", "ignore")
    except Exception:
        return ""


def harvest(html):
    emails, mobiles = set(), set()
    for m in EMAIL.findall(html):
        ml = m.lower()
        if not any(b in ml for b in BAD) and len(ml) < 60:
            emails.add(ml)
    for m in MOBILE.findall(html):
        mobiles.add(m)
    return emails, mobiles


def contact_links(html, base):
    out = []
    for href in re.findall(r'href=["\']([^"\']+)["\']', html, re.I):
        if re.search(r"contact|reach|connect|about", href, re.I):
            out.append(urllib.parse.urljoin(base, href))
    seen, uniq = set(), []
    for u in out:
        if u not in seen:
            seen.add(u); uniq.append(u)
    return uniq[:2]


def enrich(lead):
    url = lead["Website"].strip()
    if not url.startswith("http"):
        url = "http://" + url
    html = fetch(url)
    if not html:
        return lead["MemID"], set(), set()
    emails, mobiles = harvest(html)
    if not emails:  # homepage often lacks it; try a contact page
        for c in contact_links(html, url):
            e, mo = harvest(fetch(c))
            emails |= e; mobiles |= mo
            if emails:
                break
    return lead["MemID"], emails, mobiles


rows = list(csv.DictReader(open("leads.csv", encoding="utf-8")))
targets = [r for r in rows if r["Website"].strip() and r["WebStatus"] in ("live", "exists")]
print(f"sites to scrape: {len(targets)} (of {len(rows)} leads)")

results = {}
with ThreadPoolExecutor(max_workers=30) as ex:
    for i, (mid, emails, mobiles) in enumerate(ex.map(enrich, targets), 1):
        results[mid] = (emails, mobiles)
        if i % 100 == 0:
            print(f"  ...{i}/{len(targets)}")

# prefer same-domain emails
def best_email(emails, website):
    if not emails:
        return ""
    dom = re.sub(r"^www\.", "", urllib.parse.urlparse(
        website if website.startswith("http") else "http://" + website).hostname or "")
    same = [e for e in emails if dom and dom.split(".")[0] in e.split("@")[1]]
    return ", ".join(sorted(same)[:2]) if same else ", ".join(sorted(emails)[:2])


got_email = got_mobile = 0
for r in rows:
    e, mo = results.get(r["MemID"], (set(), set()))
    r["Email"] = best_email(e, r["Website"])
    r["MobileFound"] = ", ".join(sorted(mo)[:2])
    if r["Email"]:
        got_email += 1
    if r["MobileFound"]:
        got_mobile += 1

cols = list(rows[0].keys())
with open("leads_enriched.csv", "w", encoding="utf-8", newline="") as f:
    w = csv.DictWriter(f, fieldnames=cols)
    w.writeheader(); w.writerows(rows)

print(f"\nenriched -> leads_enriched.csv")
print(f"leads that gained an EMAIL : {got_email}")
print(f"leads that gained a MOBILE : {got_mobile}")
print("\nsamples:")
n = 0
for r in rows:
    if r["Email"]:
        print(f"  {r['Company'][:34]:34} | {r['Email']:38} | {r['MobileFound']}")
        n += 1
    if n >= 10:
        break

# append to the analysis knowledge base (runs every time)
import json, os, datetime
_ts = datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="seconds")
_d = os.path.dirname(os.path.abspath("leads.csv"))
with open(os.path.join(_d, "runs.jsonl"), "a", encoding="utf-8") as _f:
    _f.write(json.dumps({"ts": _ts, "tool": "enrich_web", "sites_scraped": len(targets),
                         "emails": got_email, "mobiles": got_mobile}) + "\n")
with open(os.path.join(_d, "ANALYSIS_LOG.md"), "a", encoding="utf-8") as _f:
    _f.write(f"\n### {_ts} — enrich_web (website contact scrape)\n")
    _f.write(f"- sites scraped {len(targets)} | emails gained {got_email} | mobiles gained {got_mobile}\n")
