## gg discuss note

Append a deliberation turn to a discussion transcript

### Synopsis

Records one turn in the discussion's audit transcript.
Use --by to identify the contributor and --role for their perspective.

```
gg discuss note DISC-ID "text" [flags]
```

### Options

```
      --by string     contributor name or agent role
  -h, --help          help for note
      --role string   perspective: dev|pm|architect|ux|analyst|writer|user (default "user")
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg discuss](gg_discuss.md)	 - Manage open discussions — topics raised but not yet concluded

