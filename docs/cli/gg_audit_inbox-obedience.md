## gg audit inbox-obedience

Measure inbox-obey compliance per agent role

### Synopsis

Count role-targeted messages received vs acknowledged (marked read) per agent
role over a time window. Acknowledged = marked read via 'gg inbox' (peek bypassed).

obedience_ratio = acknowledged / received

Roles with ratio < 0.5 and received > 3 are flagged as low-compliance.

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

