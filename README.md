# bills-bot

A Telegram bot that replaces the household's monthly bills sticky note, and keeps the history.

The domain language is in [CONTEXT.md](./CONTEXT.md). Decisions that would otherwise be
surprising are in [docs/adr](./docs/adr).

## Status

The standing bill list and the monthly Board work. A month can be opened, which copies every bill
onto a pinned Board with its amount unknown. Amounts cannot be entered yet, nothing can be marked
paid, and there is no scheduler and no reporting: the Board's bill buttons answer "does not work
yet", and only 🔄 Refresh does anything.

Working today:

| Command | Behaviour |
| --- | --- |
| `/start` | Explains what the bot is, in the configured group only |
| `/newmonth [YYYY-MM]` | Opens this month in Manila (or the one named), posts its Board and pins it. Opening a month that exists says so and changes nothing. Up to next month may be opened |
| `/board [YYYY-MM]` | Posts the current month's Board again at the bottom of the chat, pins it, and unpins and deletes the previous copy |
| `/bills` | Prints the standing list, grouped by section, with how each bill is paid |
| `/bills add <name> \| <section> \| <channel> [\| <card>]` | Adds a bill to the end of its section |
| `/bills section <name>` | Adds a section to the end of the display order |

Channels are written as a person would say them: `kevin`, `sheena bdo`, `sheena bpi`,
`sheena psbank`, `card`. Only a bill on `card` names the card it lands on, and every other
channel is refused one — the schema states the same rule as a CHECK.

The list is seeded from the sticky note by migration `00002`, so a fresh database already knows
the household's sixteen bills. A bill added while a month is open joins that month's Board, due
and with no amount; a closed month is left alone.

The bot must be a group admin with the **Pin messages** permission. Without it the Board is still
posted, and the bot says it could not pin it.

The gate is deliberately quiet. The bot serves exactly one group chat and exactly the Telegram
user ids on its allowlist. A stranger in the group gets no reply at all, not even a refusal, so
that the bot does not reveal itself. A private message gets an explanation only if it is
`/start`.

## Running it locally

```sh
cp .env.example .env     # then fill in the three required values
go run ./cmd/bot
```

Finding the ids you need, once the bot is in the group and someone has posted:

```sh
curl -s "https://api.telegram.org/bot<TOKEN>/getUpdates" | jq '.result[].message | {chat, from}'
```

The chat id is negative, and a supergroup's looks like `-1001234567890`.

Set `SCHEDULER_ENABLED=false` while developing, so the bot does not open cycles or post
reminders against your real group.

## Tests

```sh
go vet ./... && go test ./...
```

Schema behaviour is tested against a real migrated SQLite file rather than a mock, so the
constraints in the migration are covered: the channel enum, the rule that only a
charged-to-card bill carries a card name, case-insensitive uniqueness of active bill names with
archived names freed for reuse, one payable per bill per cycle, and one transfer per channel
per cycle.

`storagetest.Open` gives a test the database as production has it, seed included.
`storagetest.OpenEmpty` clears the seeded list, for tests that need to choose their own bills.

## Deployment

CI builds on the self-hosted runner, pushes `kevinjake/bills-bot` to Docker Hub, and pokes a
Portainer webhook. The production stack is `deploy/docker-compose.prod.yml`, a git-backed
Portainer stack on LXC 107 with the database on a bind mount at `/opt/bills-bot/data`.

Back the database up from the host:

```sh
sqlite3 /opt/bills-bot/data/bills.db ".backup '/opt/bills-bot/data/bills-backup.db'"
```
