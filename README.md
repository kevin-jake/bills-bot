# bills-bot

A Telegram bot that replaces the household's monthly bills sticky note, and keeps the history.

The domain language is in [CONTEXT.md](./CONTEXT.md). Decisions that would otherwise be
surprising are in [docs/adr](./docs/adr).

## Status

Walking skeleton. The database schema, configuration and the bot's access gate are in place and
tested. There is no Board, no bill list and no reporting yet.

Working today:

| Command | Behaviour |
| --- | --- |
| `/start` | Explains what the bot is, in the configured group only |

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

## Deployment

CI builds on the self-hosted runner, pushes `kevinjake/bills-bot` to Docker Hub, and pokes a
Portainer webhook. The production stack is `deploy/docker-compose.prod.yml`, a git-backed
Portainer stack on LXC 107 with the database on a bind mount at `/opt/bills-bot/data`.

Back the database up from the host:

```sh
sqlite3 /opt/bills-bot/data/bills.db ".backup '/opt/bills-bot/data/bills-backup.db'"
```
