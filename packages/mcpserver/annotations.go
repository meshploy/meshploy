package mcpserver

import (
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	mcpsdk "github.com/mark3labs/mcp-go/server"
)

// Every tool says what calling it can do, in the annotations MCP clients
// show and ask before acting on: a title, and whether it reads only, can lose
// something, repeats safely and reaches outside the workspace. A tool made
// with mcp.NewTool and left alone claims the worst (destructive, open world),
// so a client would ask before every list; this table is where each one is
// told the truth, in one place a reviewer can read top to bottom.
//
// The kinds:
//   - read: changes nothing.
//   - add: creates, starts or runs something, and loses nothing a later call
//     could not have back (a stopped service starts again, a paused route
//     publishes again, a deployment stays in the history).
//   - change: can lose something no later call puts back: a deletion, a value
//     or a set of values overwritten, a restore over what is there, a token
//     rotated, or a query that may write.
type toolKind int

const (
	read toolKind = iota
	add
	change
)

var toolKinds = map[string]toolKind{
	// read
	"list_resources": read, "get_resource": read, "get_env_vars": read, "get_build_env_vars": read,
	"get_build_config": read, "get_database_config": read, "db_schema": read,
	"list_deployments": read, "get_deployment": read, "list_job_runs": read, "list_service_pods": read,
	"get_metrics": read, "get_sandbox": read,
	"list_variable_groups": read, "get_variable_group": read, "list_config_files": read,
	"list_stack_services": read, "list_templates": read, "get_template": read,
	"list_org_members": read, "list_invitations": read, "list_member_permissions": read, "list_resource_permissions": read,
	"list_backup_configs": read, "list_backup_objects": read, "get_system_backup": read, "list_system_backup_objects": read,
	"list_notification_channels": read, "list_domains": read, "list_tcp_ports": read,
	"get_node_registration_token": read,
	"list_git_integrations":       read, "list_git_repos": read, "list_git_branches": read, "detect_repository": read,
	"list_registry_integrations": read, "list_storage_integrations": read, "list_dns_providers": read,

	// add
	"create_project": add, "create_service": add, "create_job": add, "create_volume": add, "create_stack": add,
	"create_variable_group": add, "create_config_file": add, "create_route": add, "create_backup_config": add,
	"create_notification_channel": add, "create_provisioning_token": add,
	"create_git_integration": add, "create_registry_integration": add, "create_storage_integration": add, "create_dns_provider": add,
	"check_dns_provider": add, "verify_route_hostname": add,
	"attach_variable_group": add, "attach_config_file": add, "attach_volume": add,
	"detach_variable_group": add, "detach_config_file": add, "detach_volume": add,
	"deploy_service": add, "deploy_template": add, "deploy_folder": add, "rollback_deployment": add,
	"retry_deployment": add, "cancel_deployment": add,
	"start_service": add, "stop_service": add, "trigger_job": add, "trigger_backup": add, "trigger_system_backup": add,
	"pause_route": add, "publish_route": add, "publish_tcp_port": add, "pause_tcp_port": add, "reopen_tcp_port": add,
	"invite_member": add, "grant_permission": add, "share_app": add,

	// change
	"update_project": change, "update_service": change, "update_build_config": change, "update_job": change,
	"update_stack": change, "update_variable_group": change, "update_config_file": change, "update_member_role": change,
	"set_env_vars": change, "set_build_env_vars": change, "set_secret": change, "set_variable": change, "set_sandbox": change,
	"upsert_system_backup": change, "apply_stack": change, "apply_manifest": change, "sync_stack": change,
	"generate_node_registration_token": change, "db_query": change,
	"restore_backup": change, "restore_system_backup": change,
	"revoke_permission": change, "remove_org_member": change, "unpublish_tcp_port": change,
	"delete_project": change, "delete_service": change, "delete_job": change, "delete_volume": change,
	"delete_stack": change, "delete_secret": change, "delete_variable": change, "delete_variable_group": change,
	"delete_config_file": change, "delete_route": change, "delete_backup_config": change, "delete_node": change,
	"delete_notification_channel": change, "delete_git_integration": change, "delete_registry_integration": change,
	"delete_storage_integration": change, "delete_dns_provider": change,
}

// openWorld are the tools that act outside the workspace: on the public
// internet (a hostname or port anyone can reach), on people (an email sent),
// or on a provider's account through its API (git, DNS, a registry, storage).
// Everything else stays inside the workspace and its own servers.
var openWorld = map[string]bool{
	"create_route": true, "publish_route": true, "publish_tcp_port": true, "reopen_tcp_port": true, "deploy_template": true,
	"invite_member": true, "share_app": true,
	"create_git_integration": true, "list_git_repos": true, "list_git_branches": true, "detect_repository": true,
	"create_dns_provider": true, "check_dns_provider": true, "delete_dns_provider": true, "verify_route_hostname": true,
	"create_registry_integration": true, "create_storage_integration": true,
}

// idempotent are the tools beyond the readers that a second identical call
// leaves as the first did: they set a state rather than add to one.
var idempotent = map[string]bool{
	"start_service": true, "stop_service": true, "pause_route": true, "publish_route": true,
	"pause_tcp_port": true, "reopen_tcp_port": true, "check_dns_provider": true, "verify_route_hostname": true,
	"update_project": true, "update_service": true, "update_build_config": true, "update_job": true,
	"update_stack": true, "update_variable_group": true, "update_config_file": true, "update_member_role": true,
	"set_env_vars": true, "set_build_env_vars": true, "set_secret": true, "set_variable": true, "set_sandbox": true,
	"upsert_system_backup": true, "apply_stack": true, "apply_manifest": true, "sync_stack": true,
	"grant_permission": true, "revoke_permission": true,
}

// titleWords are the words a tool's name says shortly that its title spells
// out; the rest of a title is the name, its first word capitalised.
var titleWords = map[string]string{
	"env": "environment", "vars": "variables", "db": "database", "dns": "DNS", "tcp": "TCP",
	"upsert": "set up", "org": "organization", "config": "config", "configs": "configs",
}

// toolTitles are titles the words alone would not make well.
var toolTitles = map[string]string{
	"db_query":                         "Run a database query",
	"db_schema":                        "Read a database's tables",
	"apply_manifest":                   "Deploy a compose manifest",
	"generate_node_registration_token": "Rotate the node registration token",
	"get_resource":                     "Get a resource",
	"list_resources":                   "List resources",
	"get_metrics":                      "Get metrics history",
	"share_app":                        "Share an app by email",
	"deploy_folder":                    "Deploy a local folder",
}

// toolTitle is the title a tool is shown under.
func toolTitle(name string) string {
	if t, ok := toolTitles[name]; ok {
		return t
	}
	words := strings.Split(name, "_")
	for i, w := range words {
		if r, ok := titleWords[w]; ok {
			words[i] = r
		}
	}
	words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	return strings.Join(words, " ")
}

// annotate gives every tool in toolKinds its annotations. A tool it does not
// name (an edition's) keeps the ones it was made with.
func annotate(ms *mcpsdk.MCPServer) {
	var out []mcpsdk.ServerTool
	for name, st := range ms.ListTools() {
		kind, ok := toolKinds[name]
		if !ok {
			continue
		}
		t := st.Tool
		t.Annotations = mcp.ToolAnnotation{
			Title:           toolTitle(name),
			ReadOnlyHint:    mcp.ToBoolPtr(kind == read),
			DestructiveHint: mcp.ToBoolPtr(kind == change),
			IdempotentHint:  mcp.ToBoolPtr(kind == read || idempotent[name]),
			OpenWorldHint:   mcp.ToBoolPtr(openWorld[name]),
		}
		out = append(out, mcpsdk.ServerTool{Tool: t, Handler: st.Handler})
	}
	ms.AddTools(out...)
}
