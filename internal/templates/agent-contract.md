## GG MANDATORY CONTRACT

gg-cli is the mandatory coordination channel for this project.

- All tasks, decisions, bugs, and broadcasts go through gg: `gg task`, `gg record`, `gg bug`, `gg tell`
- Before starting new work, run: `gg inbox`
- Never call `gsd_plan_*` tools in projects using gg — use `gg task create` instead
- Before starting any new task or reasoning step, run: `gg inbox --role $GG_ROLE --since-cursor`.
  If role-targeted unread messages exist, you MUST either:
    (a) start the referenced task via `gg task start <id>`, OR
    (b) reply with `gg tell <sender> <deferral reason>`
  Silent skip = protocol violation. It will be caught by structural gates.
