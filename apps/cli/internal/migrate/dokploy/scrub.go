package dokploy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Turning a real server's reading into a fixture.
//
// The golden plans in testdata are written by hand, which keeps them readable
// and keeps everybody's data out of the repository - but it also means they
// only cover shapes somebody thought of. A real server has the shapes nobody
// thought of, and those are the ones that break a migration.
//
// Scrub takes a reading and replaces everything that identifies the server or
// its owner, keeping the structure the plan is built from: which tables have
// rows, which columns those rows carry, how they reference each other, how big
// the data is. What comes out can be committed and read by anyone.
//
// It replaces rather than deletes, because an empty column and a scrubbed one
// plan differently: the reader branches on whether a repository, a domain or a
// mount path is present.

// secretColumns never survive, whatever they contain: Dokploy keeps tokens,
// passwords and private keys in these.
var secretColumns = map[string]bool{
	"env": true, "buildArgs": true, "composeFile": true, "customGitSSHKey": true,
	"databasePassword": true, "databaseRootPassword": true, "password": true,
	"accessToken": true, "refreshToken": true, "clientSecret": true, "secret": true,
	"appPassword": true, "apiToken": true, "githubPrivateKey": true, "webhookSecret": true,
	"registryPassword": true, "secretAccessKey": true, "accessKey": true, "sshKey": true,
}

// identityColumns name things: people's projects, hosts, repositories, paths.
// They are replaced with a stable stand-in, so two rows that named the same
// thing still name the same thing.
var identityColumns = map[string]bool{
	"name": true, "appName": true, "description": true, "host": true, "hostPath": true,
	"repository": true, "owner": true, "customGitUrl": true, "gitlabPathNamespace": true,
	"gitlabOwner": true, "gitlabRepository": true, "giteaOwner": true, "giteaRepository": true,
	"bitbucketOwner": true, "bitbucketRepository": true, "registryUrl": true, "registryName": true,
	"username": true, "email": true, "bucket": true, "endpoint": true, "serverId": true,
	"regex": true, "replacement": true, "composePath": true, "databaseName": true,
	"databaseUser": true, "serviceName": true, "volumeName": true, "title": true,
	// A stored image can be one they built and named after their app, or a
	// private registry path naming their org.
	"dockerImage": true,
}

// Scrub returns a copy of src with names, addresses and secrets replaced.
func Scrub(src Source) Source {
	s := &scrubber{names: map[string]string{}, counts: map[string]int{}}
	out := src

	out.Rows = map[string][]Row{}
	tables := make([]string, 0, len(src.Rows))
	for t := range src.Rows {
		tables = append(tables, t)
	}
	sort.Strings(tables) // stable stand-ins across runs
	for _, table := range tables {
		rows := make([]Row, 0, len(src.Rows[table]))
		for _, r := range src.Rows[table] {
			rows = append(rows, s.row(table, r))
		}
		out.Rows[table] = rows
	}

	out.Docker.Containers = nil
	for _, c := range src.Docker.Containers {
		c.Name = s.name("container", c.Name)
		c.NetworkMode = s.network(c.NetworkMode)
		c.Service = s.keep("container", c.Service)
		c.Project = s.keep("container", c.Project)
		c.Image = s.image(c.Image)
		for i, b := range c.BindSources {
			c.BindSources[i] = s.path(b)
		}
		c.Env = s.env(c.Env)
		out.Docker.Containers = append(out.Docker.Containers, c)
	}
	out.Docker.Services = nil
	for _, v := range src.Docker.Services {
		v.Name = s.keep("container", v.Name)
		v.Image = s.image(v.Image)
		v.Env = s.env(v.Env)
		out.Docker.Services = append(out.Docker.Services, v)
	}
	out.Docker.Volumes = nil
	for _, v := range src.Docker.Volumes {
		v.Name = s.volume(v.Name)
		out.Docker.Volumes = append(out.Docker.Volumes, v)
	}

	// A listener's address is the server's own: its public IP, or a mesh
	// address that says which network it is on.
	out.Listeners = nil
	for _, l := range src.Listeners {
		l.Address = s.address(l.Address)
		out.Listeners = append(out.Listeners, l)
	}

	out.PathMB = map[string]int{}
	for p, mb := range src.PathMB {
		out.PathMB[s.path(p)] = mb
	}

	// The detection's own strings name the server's Dokploy, not the server.
	out.Detection.Signals = append([]string(nil), src.Detection.Signals...)
	return out
}

// env keeps the shape of a workload's environment without its values: how many
// variables it had and what they were called, because that is what the reader
// branches on, and nothing of what they were set to. The value is a stand-in
// carrying the stand-in names already used elsewhere, so an app that reached a
// database by hostname still reaches it in the fixture.
func (s *scrubber) env(env []string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for _, line := range env {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			out = append(out, s.name("env", line))
			continue
		}
		out = append(out, key+"="+s.envValue(value))
	}
	return out
}

// envValue keeps the names of workloads a value mentions - a database host, a
// connection string's host - and replaces everything else. Those references
// are the whole reason this is read at all: they are what joins an application
// to its database.
func (s *scrubber) envValue(value string) string {
	kept := value
	for real, stand := range s.names {
		if real != "" && strings.Contains(kept, real) {
			kept = strings.ReplaceAll(kept, real, stand)
		}
	}
	if kept != value {
		return kept // it named something; the naming is what matters
	}
	return "scrubbed"
}

// scrubber keeps one stand-in per real value, so relationships survive: the
// app that referenced a database by its host name still does.
type scrubber struct {
	names      map[string]string
	counts     map[string]int
	publicAddr string
}

func (s *scrubber) row(table string, r Row) Row {
	out := Row{}
	for k, v := range r {
		switch {
		case strings.HasSuffix(k, "Id") || k == "id":
			// An id is opaque, but it is still theirs, and rows reference each
			// other by it - so it is replaced by a stand-in, the same one
			// wherever it appears.
			if str, ok := v.(string); ok && str != "" {
				out[k] = s.name("id", str)
			} else {
				out[k] = v
			}
		case secretColumns[k]:
			if str, ok := v.(string); ok && str != "" {
				out[k] = s.secret(k, str)
			} else {
				out[k] = v
			}
		case identityColumns[k]:
			if str, ok := v.(string); ok && str != "" {
				out[k] = s.value(table, k, str)
			} else {
				out[k] = v
			}
		default:
			out[k] = v
		}
	}
	return out
}

// secret keeps the shape - how many lines an env block had, that a key was a
// key - without keeping anything of the value.
func (s *scrubber) secret(column, value string) string {
	if column == "env" || column == "buildArgs" {
		var lines []string
		for i, line := range strings.Split(value, "\n") {
			name, _, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok || name == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("VAR_%d=value", i+1))
		}
		return strings.Join(lines, "\n")
	}
	if column == "composeFile" {
		return "# a compose file with " + fmt.Sprint(strings.Count(value, "\n")+1) + " lines\n"
	}
	return "scrubbed"
}

// value replaces a name with a stable stand-in of the same kind.
func (s *scrubber) value(table, column, value string) string {
	switch column {
	case "host":
		return s.host(value)
	case "hostPath", "composePath":
		return s.path(value)
	case "customGitUrl":
		return "https://git.example/" + s.name("repo", value) + ".git"
	case "endpoint":
		return "https://storage.example"
	case "email":
		return s.name("person", value) + "@example.com"
	case "regex", "replacement":
		// A redirect's regex is built from hostnames, which are scrubbed by the
		// same map, so the rule still points where it pointed.
		return hostPattern.ReplaceAllStringFunc(value, s.host)
	case "appName", "serviceName", "volumeName":
		return s.keep("container", value)
	case "dockerImage":
		return s.image(value)
	}
	return s.name(table, value)
}

// keepImages are the bases the reader branches on - a database's engine is read
// from its image. Everything else is replaced: an image can be built from
// somebody's code and named after it, and a registry path names its owner.
var keepImages = map[string]bool{
	"postgres": true, "mysql": true, "mariadb": true, "mongo": true, "mongodb": true,
	"redis": true, "dokploy/dokploy": true, "traefik": true,
}

func (s *scrubber) image(image string) string {
	if image == "" {
		return ""
	}
	name, tag, ok := strings.Cut(image, ":")
	if !ok {
		tag = "latest"
	}
	if keepImages[name] {
		return image
	}
	return s.name("image", name) + ":" + s.tag(tag)
}

// tag keeps a version and drops a digest, which is unique to their build.
func (s *scrubber) tag(tag string) string {
	if v, _, ok := strings.Cut(tag, "@"); ok {
		return v
	}
	return tag
}

// hostPattern matches a hostname inside a larger string, such as a redirect's
// regular expression.
var hostPattern = regexp.MustCompile(`[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+`)

func (s *scrubber) host(host string) string {
	if strings.HasSuffix(host, ".traefik.me") {
		// Dokploy generates these, and their shape is what the plan reads.
		return s.name("host", host) + "-192-0-2-1.traefik.me"
	}
	sub := s.name("host", host)
	if strings.Count(host, ".") <= 1 {
		return sub + ".example"
	}
	return sub + ".apps.example"
}

func (s *scrubber) path(p string) string {
	if p == "" || strings.HasPrefix(p, "/etc/dokploy") {
		return p // Dokploy's own layout, which the reader branches on
	}
	return "/srv/" + s.name("path", p)
}

// address keeps the addresses that mean something to the reader - every
// interface, loopback - and replaces the ones that identify this machine.
// 198.51.100.x is TEST-NET-2, reserved for documentation.
func (s *scrubber) address(addr string) string {
	switch addr {
	case "", "0.0.0.0", "127.0.0.1", "::", "::1", "[::]":
		return addr
	}
	if strings.HasPrefix(addr, "100.64.") || strings.HasPrefix(addr, "100.1") {
		return "100.64.0.1" // the mesh, which every gateway shares
	}
	if s.publicAddr == "" {
		s.publicAddr = "198.51.100.10"
	}
	return s.publicAddr
}

// network keeps docker's own modes, which the plan reads, and replaces a
// compose network, which is named after somebody's project.
func (s *scrubber) network(mode string) string {
	switch {
	case mode == "", mode == "host", mode == "bridge", mode == "none", mode == "default":
		return mode
	case strings.HasPrefix(mode, "container:"):
		return "container:" + s.name("container", strings.TrimPrefix(mode, "container:"))
	}
	// A compose network is "<project>_default", and the project is an app.
	if base, rest, ok := strings.Cut(mode, "_"); ok {
		return s.keep("container", base) + "_" + rest
	}
	return s.keep("container", mode)
}

// volume keeps a volume attached to the workload that owns it.
//
// A compose project's volumes are "<appName>_<volume>", and the plan attributes
// them by that prefix. Renaming the whole string would leave a fixture whose
// volumes belong to nobody - the plan would still build, and would quietly be
// wrong about what a group copies.
func (s *scrubber) volume(name string) string {
	owner, standin := s.longestKnown("container", name)
	if owner == "" {
		return s.name("volume", name)
	}
	// The remainder names the compose service, so it is replaced too - only the
	// separator, which the plan matches on, is kept.
	rest := name[len(owner):]
	sep := ""
	if rest != "" {
		sep, rest = rest[:1], rest[1:]
	}
	return standin + sep + s.name("volume", rest)
}

// longestKnown finds the longest value of this kind that name starts with,
// and its stand-in.
func (s *scrubber) longestKnown(kind, name string) (string, string) {
	prefix := kind + "\x00"
	best, standin := "", ""
	for key, got := range s.names {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		value := key[len(prefix):]
		if value != "" && strings.HasPrefix(name, value) && len(value) > len(best) {
			best, standin = value, got
		}
	}
	return best, standin
}

// name returns this value's stand-in, making one on first sight.
func (s *scrubber) name(kind, value string) string {
	if value == "" {
		return ""
	}
	// Dokploy's own components are the product's names, not the owner's, and
	// the reader knows them: scrubbing "dokploy-traefik" made the plan report
	// Dokploy's own containers as workloads somebody should import.
	if kind == "container" && (value == "dokploy" || strings.HasPrefix(value, "dokploy-") || strings.HasPrefix(value, "dokploy.")) {
		return value
	}
	key := kind + "\x00" + value
	if got, ok := s.names[key]; ok {
		return got
	}
	s.counts[kind]++
	got := fmt.Sprintf("%s-%d", kind, s.counts[kind])
	s.names[key] = got
	return got
}

// keep is name with the kind shared across tables, so a database's appName and
// the container running it come out as the same stand-in.
func (s *scrubber) keep(kind, value string) string { return s.name(kind, value) }
