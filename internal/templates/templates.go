package templates

import _ "embed"

//go:embed docker-compose.yaml
var DockerCompose string

//go:embed rules.md
var RulesMD string

//go:embed agents.md
var AgentsMD string

//go:embed config.yaml
var ConfigYAML string

//go:embed task-done-go.sh
var TaskDoneGoHook string

//go:embed pre-task-done-go.sh
var PreTaskDoneGoHook string

//go:embed pre-task-done-node.sh
var PreTaskDoneNodeHook string

//go:embed 90-bug-repros.sh
var BugReprosHook string
