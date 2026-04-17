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
