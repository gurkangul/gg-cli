package templates

import _ "embed"

//go:embed docker-compose.yaml
var DockerCompose string

//go:embed rules.md
var RulesMD string

//go:embed config.yaml
var ConfigYAML string
