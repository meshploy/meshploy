module github.com/meshploy/apps/cli

go 1.25.8

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/spf13/cobra v1.10.2
	golang.org/x/term v0.42.0
)

require (
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/text v0.34.0 // indirect
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mark3labs/mcp-go v0.52.0
	github.com/meshploy/packages/client v0.0.0
	github.com/meshploy/packages/mcpserver v0.0.0
	github.com/spf13/pflag v1.0.10 // indirect
	golang.org/x/sys v0.43.0 // indirect
)

replace github.com/meshploy/packages/client => ../../packages/client

replace github.com/meshploy/packages/mcpserver => ../../packages/mcpserver
