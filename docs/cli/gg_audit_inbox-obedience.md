## gg audit inbox-obedience

Measure role-targeted handoff acknowledgement per agent role

### Synopsis

Count role-targeted messages received vs acknowledged (marked read) per agent
role over a time window. Acknowledged = marked read via 'gg inbox' (peek bypassed).

handoff_ack_ratio = acknowledged / actionable role-targeted messages

Roles with ratio < 0.5 and actionable > 3 are flagged because future agents may
be missing durable blockers, review requests, or evidence handoffs. The JSON
field remains "obedience_ratio" for backward compatibility.

```
gg audit inbox-obedience [flags]
```

### Options

```
  -h, --help           help for inbox-obedience
      --json           emit machine-readable JSON
      --role string    filter to a specific recipient role
      --since string   time window (7d, 24h, 30d, or RFC3339 timestamp) (default "7d")
```

### SEE ALSO

* [gg audit](gg_audit.md)	 - Session mutation audit (called by PostToolUse and Stop hooks)
