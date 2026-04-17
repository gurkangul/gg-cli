## gg discuss

Manage open discussions (deprecated — use gg message or gg record)

### Synopsis

Discussions track conversations that haven't reached a decision/task/rejection.

DEPRECATED: usage data shows discuss is underused (0 calls in dogfood).
  - For deliberation across agents: use 'gg message send' / 'gg tell'
  - For capturing a concluded outcome: use 'gg record' (decision) or 'gg task'
  - For async coordination: use 'gg message' with --thread

This command will be removed in a future release (v0.2+).
TODO(v0.2): remove gg discuss

### Options

```
  -h, --help   help for discuss
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg discuss dismiss](gg_discuss_dismiss.md)	 - Close a discussion without a resolution (topic superseded or irrelevant)
* [gg discuss get](gg_discuss_get.md)	 - Show a single discussion
* [gg discuss list](gg_discuss_list.md)	 - List discussions (default: open only)
* [gg discuss note](gg_discuss_note.md)	 - Append a deliberation turn to a discussion transcript
* [gg discuss open](gg_discuss_open.md)	 - Open a new discussion (unresolved topic)
* [gg discuss resolve](gg_discuss_resolve.md)	 - Close a discussion by linking it to a decision/task/rejection
* [gg discuss show](gg_discuss_show.md)	 - Show a discussion with optional full transcript

