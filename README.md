# stonks

Your Sharesight portfolios in the terminal. Go, single binary, read-only (GET requests only;
Sharesight's API keys can also write, so nothing here ever POSTs/PUTs/DELETEs data).

    stonks                     interactive: Combined tab + a tab per portfolio, enter on a holding for detail
                               (in a pipe or script it prints the summary instead)
    stonks summary             summary for every period (today ... all time)
    stonks holdings [-s KEY]   all holdings; sort: value (default) today 1m 1y all code
    stonks show CODE           one holding in detail
    stonks chart [PERIOD]      performance chart: 1w 1m ytd 1y 3y 5y all
    stonks history [N]         daily snapshots
    stonks pull                save today's snapshot (cron, weekdays 17:30)
    -p NAME  one portfolio     --fresh  skip the 10-minute cache

- Credentials: `~/.config/sharesight/credentials` (mode 600), `SHARESIGHT_CLIENT_ID=` / `SHARESIGHT_CLIENT_SECRET=`.
- Cache and token: `~/.cache/stonks/`. Snapshots: `~/.local/share/stonks/snapshots.jsonl`.
- Sharesight returns per-annum figures for any period over a year (account setting; the API flag is ignored).
- Sharesight answers 403 to bursts of requests; the client runs 2 at a time and backs off on 403.
- Build/install: `go build -o stonks . && install -m 755 stonks ~/.local/bin/stonks`
- Display only: figures, never buy/sell suggestions. Data stays on this machine.
