# CLAUDE.md

`stonks`: the user's Sharesight portfolios in the terminal (Go, built 2026-11-19 from Master Control). Read README.md first.

- **Read-only, always.** Sharesight's API keys can also write. Only ever make GET requests to /api/v3 (plus the OAuth token POST).
- **Never** print, log or commit the credentials (~/.config/sharesight/credentials) or the cached token (~/.cache/stonks/token.json).
- Portfolio data is private: it stays on this machine. Don't put it on DigitalOcean, snookmz.click, published artifacts or anything else that leaves the network.
- Figures only: no buy/sell suggestions or investment advice.
- The user prefers large, readable output.
- Sharesight returns 403 on bursts of requests; keep concurrency low (2) with backoff.
- Periods over a year come back p.a. whatever flag is sent.
- Install after changes: `go build -o stonks . && install -m 755 stonks ~/.local/bin/stonks`. Cron runs `stonks pull` on weekdays at 17:30.
- `~/src/stonks` is a different project (Equishark). Don't touch it from here.
- New repos use the `master` branch.
