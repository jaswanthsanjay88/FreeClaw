# WAReplyMate Channel

This channel integrates PicoClaw with a WAReplyMate deployment instead of PicoClaw's built-in WhatsApp bridge mode.

## How It Works

- Inbound: polls WAReplyMate's `messages.db` SQLite store for new incoming messages.
- Outbound: sends replies through WAReplyMate HTTP API (`POST /api/send`).
- Filtering: skips self messages (`is_from_me = 1`) to avoid response loops.
- Owner policy: owner JIDs can use full agent behavior (tools/tasks + long-lived context).
- Non-owner policy: can be forced into reply-only mode (no tool execution) with temporary context windows.

## Configuration

```json
{
  "channels": {
    "wareplymate": {
      "enabled": true,
      "api_base_url": "http://localhost:8080/api",
      "messages_db_path": "E:/WAReplyMate-main/whatsapp-mcp-local/whatsapp-bridge/store/messages.db",
      "poll_interval_seconds": 1,
      "owner_jids": ["918088997070@s.whatsapp.net", "118971063918781@lid"],
      "non_owner_no_tools": true,
      "non_owner_context_hours": 6,
      "allow_from": [],
      "reasoning_channel_id": ""
    },
    "whatsapp": {
      "enabled": false
    }
  }
}
```

## Notes

- Keep `channels.whatsapp.enabled` set to `false` when using `wareplymate`.
- Ensure WAReplyMate bridge API is reachable at `api_base_url`.
- Ensure `messages_db_path` points to the active SQLite DB file used by WAReplyMate.
- `owner_jids` are normalized automatically (both `number@s.whatsapp.net` and `number@lid` forms work).
- When `non_owner_no_tools` is `true`, non-owner users get informative replies only (no tool/task execution).
- `non_owner_context_hours` controls non-owner context window bucket size. Example: `6` means context resets every 6 hours.
