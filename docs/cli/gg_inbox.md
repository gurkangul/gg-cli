## gg inbox

Read unread messages

### Synopsis

Show unread messages in the agent inbox.

By default, agent-to-agent broadcast messages (audience=agents) are hidden.
Use --include-agents to see them.

Examples:
  gg inbox --role reviewer --peek # safe role-scoped read without marking global read state
  gg inbox --include-agents       # include agent-to-agent status broadcasts
  gg inbox --since 2h             # only messages from last 2 hours
  gg inbox --role reviewer --since-cursor --advance-cursor
                                  # cursor read; requires --role and is incompatible with --peek
  gg inbox --older-than 7d        # dismiss messages older than 7 days
  gg inbox --dismiss-all          # mark all unread messages as read without printing them
  gg inbox --group-by sender      # group messages by sender role

```
gg inbox [flags]
```

### Options

```
      --advance-cursor      after render, advance cursor; requires --role and is ignored with --peek
      --compact             one line per message — drops timestamp precision and action-required split to preserve agent context window
      --dismiss-all         mark all unread messages as read without printing them
      --group-by string     group output by field: sender
  -h, --help                help for inbox
      --include-agents      show agent-to-agent broadcasts (hidden by default)
      --older-than string   dismiss (mark read) messages older than duration without showing them
      --peek                view messages without marking as read
      --role string         filter by recipient role
      --since string        only show messages newer than duration (e.g. 2h, 7d, 30m)
      --since-cursor        only show messages newer than the stored per-agent cursor
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

