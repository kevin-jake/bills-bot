# SQLite is the system of record

Every Bill, Cycle, Payable, Transfer and audit event lives only in the bot's SQLite database,
managed by goose migrations and reached through GORM, the same stack as credit-card-tracker-bot.
The Board is edited in place on every tap, which needs a consistent transactional view of a
Cycle, and CSV export already covers getting the data back out.

## Considered Options

- **Google Sheets as the system of record**, as in credit-card-tracker-bot (see its ADR 0001).
  Rejected here: that bot appends immutable expense rows, whereas this one rewrites a Cycle's
  state many times a month and must read it back atomically to re-render the Board. Sheets has
  no transactions, a write quota of roughly sixty calls a minute, and latency on every read.
  There is also no spreadsheet-editing use case: nobody wants to open this in Sheets.
- **SQLite (chosen).** One file on a bind mount, real transactions, trivially backed up.

## Consequences

- Backups are the operator's job, via a host cron running `.backup`.
- Corrections happen through the bot's undo, or `sqlite3` on the host, never in a spreadsheet.
- Foreign keys are enforced explicitly through a connection pragma, because SQLite leaves them
  off by default.
