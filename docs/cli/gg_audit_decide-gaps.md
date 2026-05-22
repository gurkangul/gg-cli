## gg audit decide-gaps

Flag gg messages containing decision-language but no corresponding gg record

### Synopsis

Scan gg tell messages in the look-back window for decision-shaped text
(e.g. "decided to", "going with", "we chose", "rejected because") and
cross-reference against gg record/decide calls created in the same window.
Messages that look like decisions but have no matching gg record are flagged.

Non-blocking — exits 0 even when gaps are found.

```
gg audit decide-gaps [flags]
```

### Options

```
      --compact        one line per flagged message — no detail
  -h, --help           help for decide-gaps
      --since string   look back window (e.g. 7d, 14d, 30d) (default "7d")
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg audit](gg_audit.md)	 - Session mutation audit (called by PostToolUse and Stop hooks)
