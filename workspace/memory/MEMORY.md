# MEMORY

Persistent workspace memory for decisions, facts, and stable preferences.

## Stable Facts

- Repository: `e:\FreeClaw\freeclaw`
- Main runtime command: `go run ./cmd/freeclaw gateway`
- Preferred WhatsApp integration: WAReplyMate channel
- GitHub repo: `https://github.com/jaswanthsanjay88/FreeClaw.git`

## User Preferences

- Wants direct execution, not only plans.
- Prefers concise responses.
- Prioritizes practical fixes and successful push/deploy outcomes.

## Operational Notes

- If push fails with secret scanning, sanitize literals and retry.
- Verify branch/remote before force updates.
- Keep owner chat responses clean (avoid tool-call leakage).

## Update Rules

- Add only durable facts.
- Remove obsolete entries when behavior changes.
- Keep this file short and high-signal.