# Lead-gen analysis log

Append-only knowledge base for the Hosto lead pipeline. Curated insights at the
top; every scrape and enrichment run appends a dated block under "Run log"
automatically. Read this before planning outreach or changing the pipeline.

## Curated insights

### Source reality (delhichamber.com)
- Old-school chamber directory: gives company name, address, named director, and an OFFICE LANDLINE. Emails are hidden behind a contact form (0 scrapeable). Only ~15% of listings carry a mobile number.
- Verdict: excellent for company discovery and a phone/cold-call list; weak for WhatsApp or email blasts.

### Channel reality (the important one)
- The companies that most need Hosto's flagship product (a website) are the hardest to reach digitally, precisely because they have no website and therefore no scrapeable email or mobile. The no-site segment came back with 0 emails and only ~20% mobiles.
- Website-contact enrichment only works for companies that ALREADY have a site (i.e. not the website-sale prospects). It is still useful for marketing compliance reminders and WhatsApp CRM to them.
- GTM implication: selling websites to no-site SMBs is a phone / referral / field motion, not a cold-email or WhatsApp motion.

### Best ICP found
- Food / agro / seafood / frozen exporters with no website: registered Pvt Ltd (compliance hook), export business (needs a buyer-facing site plus WhatsApp catalogue), zero web presence. Dense and coherent; worth a focused campaign.

### Dead-site signal
- Raw "dead" status is noisy: it mixes directory URL typos, big brands that block scrapers, and genuinely expired domains. DNS-verify to split. First full run: 215 expired (real recovery opportunities), 84 resolve (block or path), 9 typos.
- Caveat: a live-but-hijacked domain reads as "live". A.K. Engg's castor-wheels.com now serves a Chinese competitor, and scraping it yields the squatter's email. Spot-check emails on previously-flagged domains.

### Tooling ceiling
- Free stack (mcp-server-fetch + Go scraper + Claude) covers: directory discovery, static-page parsing, company-website scraping, landlines, rule-based scoring, website-contact enrichment.
- Wall: mobile and email for no-site companies live on Justdial / IndiaMART / Google Business, which are JS-rendered and bot-protected. Needs Playwright (partial, slow, ToS-grey) or paid data. This is the one genuinely hard piece.

## Run log

### 2026-06-20 (backfilled) — leadgen crawl (delhichamber.com, A-Z)
- discovered 1290 | total 1290 | checksites=true
- web status: none 425, dead 310, live 555
- fit tags: compliance 713, whatsapp-crm 776, website+hosting 425, website-recovery 310, fresh-setup 3

### 2026-06-20 (backfilled) — enrich (dead-site verify + openers)
- dead verification: expired 215, exists 84, typo 9
- actionable outreach: 347 (domain-recovery 196, no-site/generic 110, no-site/food 25, no-site/metals 9, no-site/pharma 7)

### 2026-06-20 (backfilled) — enrich_web (website contact scrape)
- sites scraped 555 | emails gained 362 | mobiles gained 280
- coverage by segment: live-site 362 email / 304 mobile; no-site 0 email / 86 mobile; dead 0 email / 38 mobile
- overall contactable: 553 of 1290 (email 362, mobile 428, both 237)

### 2026-06-20T06:05:34Z — next (batch delivered)
- delivered 50 (50 reachable) -> batches/batch-20260620-0605.xlsx | 1231 fresh remaining

### 2026-06-20T06:05:34Z — next (batch delivered)
- delivered 50 (50 reachable) -> batches/batch-20260620-0605.xlsx | 1181 fresh remaining

### 2026-06-21T11:44:28Z — crawl crawl (directoryiia, letters=ABCDEFGHIJKLMNOPQRSTUVWXYZ)
- discovered 193 | new this run 193 | total 1474 | checksites=true
- web status: dead 353, live 576, none 545
- fit tags: compliance 758, fresh-setup 3, website+hosting 545, website-recovery 353, whatsapp-crm 885

### 2026-06-21T12:38:47Z — crawl crawl (fiafaridabad, letters=ABCDEFGHIJKLMNOPQRSTUVWXYZ)
- discovered 302 | new this run 298 | total 1772 | checksites=false
- web status: dead 353, live 576, none 545, unchecked 298
- fit tags: compliance 931, fresh-setup 3, website+hosting 545, website-recovery 353, whatsapp-crm 893
