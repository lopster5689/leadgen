#!/usr/bin/env python3
"""Shortlist enrichment pass over leads.csv:
  1. Verify 'dead' websites via DNS: expired (real recovery opportunity) vs
     typo (malformed URL in the directory) vs exists (resolves; block/path).
  2. Draft a tailored, vertical-specific outreach opener per actionable lead.
Outputs leads_outreach.csv (and .xlsx if openpyxl is available)."""
import csv, re, socket
from concurrent.futures import ThreadPoolExecutor
from urllib.parse import urlparse

rows = list(csv.DictReader(open("leads.csv", encoding="utf-8")))

# dedupe by normalized company name
seen = {}
for r in rows:
    k = " ".join(r["Company"].upper().replace(".", "").split())
    if k and k not in seen:
        seen[k] = r
rows = list(seen.values())


def host(url):
    u = url.strip()
    if not u:
        return ""
    if "://" not in u:
        u = "http://" + u
    try:
        return (urlparse(u).hostname or "").strip().strip(".")
    except ValueError:
        return ""


def malformed(h):
    return (not h) or ("." not in h) or (" " in h) or h.startswith("qqq.")


socket.setdefaulttimeout(4)


def resolves(h):
    try:
        socket.getaddrinfo(h, 80)
        return True
    except OSError:
        return False


# classify the dead bucket
dead = [r for r in rows if r["WebStatus"] == "dead"]
hosts = {r["MemID"]: host(r["Website"]) for r in dead}


def classify(r):
    h = hosts[r["MemID"]]
    if malformed(h):
        return "typo"
    return "exists" if resolves(h) else "expired"


with ThreadPoolExecutor(max_workers=40) as ex:
    kinds = dict(zip([r["MemID"] for r in dead], ex.map(classify, dead)))
for r in rows:
    r["DeadKind"] = kinds.get(r["MemID"], "")

dk = {}
for r in dead:
    dk[kinds[r["MemID"]]] = dk.get(kinds[r["MemID"]], 0) + 1
print("dead-site verification:", dk)


def salutation(d):
    d = re.sub(r"\(.*?\)", "", d.split("-")[0]).strip().strip(".,")
    parts = d.split()
    return " ".join(w.capitalize() for w in parts[:3]) if parts else "Sir/Madam"


def vertical(r):
    # the company name carries the product noun far more often than BusinessType,
    # which is frequently just "Exporter"/"Manufacturer"
    b = (r["Company"] + " " + r["BusinessType"]).lower()
    if any(x in b for x in ("food", "seafood", "frozen", "rice", "spice", "agro", "meat", "protein", "grain", "tea", "dairy")):
        return "food"
    if any(x in b for x in ("pharma", "chem", "drug", "cosmetic", "perfume", "health", "lab")):
        return "pharma"
    if any(x in b for x in ("metal", "steel", "engg", "engineer", "machin", "wheel", "equipment", "tool", "industr", "auto")):
        return "metals"
    return "generic"


NOSITE = {
    "food": ("Namaste {name}, Roshan here from Hosto. I found {company} in the Delhi Chamber export directory and noticed you don't have a website yet. "
             "Overseas food buyers vet suppliers online before ordering, so right now those enquiries go to competitors who show up. "
             "We set up a professional site with a product catalogue, reliable hosting, and a WhatsApp ordering line, and we send your MCA/ROC compliance reminders so filings never slip. "
             "Can I send a 2 minute sample built for {company}?"),
    "pharma": ("Namaste {name}, Roshan here from Hosto. I came across {company} in the Delhi Chamber directory and saw you have no website yet. "
               "Buyers and distributors in pharma and chemicals check credibility online first, and without a site {company} looks invisible to them. "
               "We build a clean site with your product range, secure hosting, and a WhatsApp enquiry line, plus MCA/ROC compliance reminders so annual filings stay on track. "
               "Shall I send a quick sample for {company}?"),
    "metals": ("Namaste {name}, Roshan here from Hosto. I found {company} in the Delhi Chamber directory and noticed there's no website yet. "
               "B2B buyers and dealers search online before they enquire, so a manufacturer without a site loses orders to ones that have one. "
               "We set up a site with your product and capability details, reliable hosting, and a WhatsApp enquiry line, and we handle your MCA/ROC compliance reminders too. "
               "Can I send a 2 minute sample for {company}?"),
    "generic": ("Namaste {name}, Roshan here from Hosto. I came across {company} in the Delhi Chamber directory and noticed you don't have a website yet. "
                "Customers look you up online before they call, so no site means lost enquiries. "
                "We build a professional site with reliable hosting and a WhatsApp business line, and we send MCA/ROC compliance reminders so filings never slip. "
                "May I send a quick sample for {company}?"),
}
DEAD = ("Namaste {name}, Roshan here from Hosto. {company}'s listed website ({domain}) is no longer working, so anyone searching for you online hits a dead link, "
        "and an expired domain can be picked up by someone else. We can rebuild it on reliable hosting, add a WhatsApp enquiry line, and set up MCA/ROC compliance reminders. "
        "Want me to check if {domain} is still available to recover and send a sample?")


def actionable(r):
    has_phone = bool(r["Phone"].strip())
    nosite = r["WebStatus"] == "none" and "compliance" in r["Tags"]
    expired = r.get("DeadKind") == "expired"
    return has_phone and (nosite or expired)


out = []
for r in rows:
    if not actionable(r):
        continue
    name = salutation(r["Director"])
    company = r["Company"].title()
    if r.get("DeadKind") == "expired":
        seg, reason = "domain-recovery", "listed domain expired (recoverable)"
        msg = DEAD.format(name=name, company=company, domain=host(r["Website"]))
    else:
        v = vertical(r)
        seg, reason = f"no-site/{v}", "no web presence"
        msg = NOSITE[v].format(name=name, company=company)
    out.append({
        "Company": r["Company"], "Director": r["Director"], "Phone": r["Phone"],
        "Address": r["Address"], "BusinessType": r["BusinessType"],
        "Segment": seg, "Reason": reason, "Outreach": msg,
    })

# sort: recovery first (urgency), then no-site
out.sort(key=lambda r: (0 if r["Segment"] == "domain-recovery" else 1, r["Company"]))

cols = ["Company", "Director", "Phone", "Address", "BusinessType", "Segment", "Reason", "Outreach"]
with open("leads_outreach.csv", "w", encoding="utf-8", newline="") as f:
    w = csv.DictWriter(f, fieldnames=cols)
    w.writeheader()
    w.writerows(out)

try:
    from openpyxl import Workbook
    from openpyxl.styles import Font, PatternFill, Alignment
    wb = Workbook(); ws = wb.active; ws.title = "Outreach"
    ws.append(cols)
    for r in out:
        ws.append([r[c] for c in cols])
    for c in ws[1]:
        c.font = Font(bold=True); c.fill = PatternFill("solid", fgColor="DCE6F1")
    ws.freeze_panes = "A2"
    ws.auto_filter.ref = ws.dimensions
    widths = {"A": 34, "B": 26, "C": 24, "D": 40, "E": 22, "F": 18, "G": 26, "H": 95}
    for col, wdt in widths.items():
        ws.column_dimensions[col].width = wdt
    for row in ws.iter_rows(min_row=2):
        row[7].alignment = Alignment(wrap_text=True, vertical="top")
    wb.save("leads_outreach.xlsx")
    xlsx = "leads_outreach.xlsx"
except ImportError:
    xlsx = "(openpyxl not installed; CSV only)"

print(f"actionable outreach leads: {len(out)}  ->  leads_outreach.csv / {xlsx}")
seg = {}
for r in out:
    seg[r["Segment"]] = seg.get(r["Segment"], 0) + 1
print("by segment:", dict(sorted(seg.items())))
print("\n================ SAMPLE OPENERS ================")
shown = set()
for r in out:
    if r["Segment"] in shown:
        continue
    shown.add(r["Segment"])
    print(f"\n[{r['Segment']}] {r['Company']}  ({r['Phone'][:24]})")
    print("  " + r["Outreach"])
    if len(shown) >= 6:
        break

# append to the analysis knowledge base (runs every time)
import json, os, datetime
_ts = datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="seconds")
_d = os.path.dirname(os.path.abspath("leads.csv"))
with open(os.path.join(_d, "runs.jsonl"), "a", encoding="utf-8") as _f:
    _f.write(json.dumps({"ts": _ts, "tool": "enrich", "dead_verify": dk,
                         "outreach_leads": len(out), "by_segment": seg}) + "\n")
with open(os.path.join(_d, "ANALYSIS_LOG.md"), "a", encoding="utf-8") as _f:
    _f.write(f"\n### {_ts} — enrich (dead-site verify + openers)\n")
    _f.write(f"- dead verification: {dk}\n")
    _f.write(f"- actionable outreach: {len(out)} ({seg})\n")
