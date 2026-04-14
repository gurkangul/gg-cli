## gg inbox

Read unread messages

### Synopsis

Show unread messages in the agent inbox.

Examples:
  gg inbox                        # show all unread, mark as read
  gg inbox --peek                 # view without marking as read
  gg inbox --since 2h             # only messages from last 2 hours
  gg inbox --older-than 7d        # dismiss messages older than 7 days
  gg inbox --dismiss-all          # mark all unread as read, no output
  gg inbox --group-by sender      # group messages by sender role

```
gg inbox [flags]
```

### Options

```
      --dismiss-all         mark all unread messages as read without printing them
      --group-by string     group output by field: sender
  -h, --help                help for inbox
      --older-than string   dismiss (mark read) messages older than duration without showing them
      --peek                view messages without marking as read
      --role string         filter by recipient role
      --since string        only show messages newer than duration (e.g. 2h, 7d, 30m)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

