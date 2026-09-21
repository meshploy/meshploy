package dokploy

import (
	"fmt"
	"strings"
)

// The connections a platform holds to the rest of the world: where it pulls
// images from, where it pushes backups to, and how it reaches a git host.
//
// Two of the three carry credentials that are the operator's, not the
// platform's - a registry's username and password, an S3 key pair - and those
// move as they are. The third mostly does not: a GitHub App, a GitLab or Gitea
// OAuth application is registered against a callback URL on the server being
// replaced, so its credentials are worth nothing here. The exception is
// Bitbucket, where Dokploy holds an Atlassian app password or API token that
// belongs to the account rather than to the server.
//
// What cannot move is not lost. Every service keeps the repository and branch
// it builds from, so one reconnect in Meshploy restores builds for all of them
// - which is why a git provider that cannot come across is reported rather than
// refused.

// RegistrySpec is a registry to create.
type RegistrySpec struct {
	Name     string
	Endpoint string
	Username string
	Password string
	// Namespace is the path images are pushed under, where the registry has
	// one: Dokploy calls it the image prefix.
	Namespace string
}

// StorageSpec is an object store to create.
type StorageSpec struct {
	Name      string
	Provider  string
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
}

// GitSpec is a git connection that can be carried across whole.
type GitSpec struct {
	Provider string
	Name     string
	BaseURL  string
	// Groups scopes which repositories are listed: a Bitbucket workspace, a
	// GitLab group, a Gitea organisation.
	Groups string
	Token  string
}

// BackupSpec is one schedule to recreate.
type BackupSpec struct {
	// ServiceID is the Meshploy database this backs up, StorageID where it
	// writes.
	ServiceID string
	StorageID string
	Schedule  string
	// Retention is how many days to keep, from Dokploy's count of copies where
	// it has one.
	Retention int
	Prefix    string
	Enabled   bool
}

// registrySpec reads one of Dokploy's registry rows.
func registrySpec(r Row) RegistrySpec {
	return RegistrySpec{
		Name:      r.Str("registryName"),
		Endpoint:  r.Str("registryUrl"),
		Username:  r.Str("username"),
		Password:  r.Str("password"),
		Namespace: r.Str("imagePrefix"),
	}
}

// storageSpec reads one of Dokploy's destination rows.
func storageSpec(r Row) StorageSpec {
	provider := strings.ToLower(r.Str("provider"))
	if provider == "" {
		provider = "s3"
	}
	return StorageSpec{
		Name:      r.Str("name"),
		Provider:  provider,
		Endpoint:  r.Str("endpoint"),
		Region:    r.Str("region"),
		Bucket:    r.Str("bucket"),
		AccessKey: r.Str("accessKey"),
		SecretKey: r.Str("secretAccessKey"),
	}
}

// gitSpec is the git connection for a provider row, or ok=false when it cannot
// be carried across.
//
// Only Bitbucket can: Dokploy holds an app password or API token there, which
// belongs to the account. A GitHub App and a GitLab or Gitea OAuth application
// are registered against this server's callback, and re-using their credentials
// somewhere else does not work - the operator connects one in Meshploy instead,
// once, and every service that builds from that host resumes.
func gitSpec(src Source, providerID, name string) (GitSpec, bool) {
	for _, r := range src.Rows["bitbucket"] {
		if r.Str("gitProviderId") != providerID {
			continue
		}
		token := r.Str("apiToken")
		if token == "" {
			token = r.Str("appPassword")
		}
		user := r.Str("bitbucketUsername")
		if token == "" || user == "" {
			return GitSpec{}, false
		}
		return GitSpec{
			Provider: "bitbucket",
			Name:     name,
			// Bitbucket authenticates as the user holding the token, and
			// Meshploy takes that as "user:token" - the shape its client uses.
			Token:  user + ":" + token,
			Groups: r.Str("bitbucketWorkspaceName"),
		}, true
	}
	return GitSpec{}, false
}

// backupSpecs reads Dokploy's schedules for one database.
func backupSpecs(src Source, itemID string) []Row {
	var out []Row
	for _, r := range src.Rows["backup"] {
		for _, key := range []string{"postgresId", "mysqlId", "mariadbId", "mongoId", "composeId"} {
			if r.Str(key) != "" && r.Str(key) == itemID {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// retentionDays turns Dokploy's "keep this many copies" into Meshploy's "keep
// for this many days".
//
// They are not the same thing and cannot be made so: a daily schedule keeping
// seven copies is a week, an hourly one keeping seven is a morning. The cron is
// read for how often it runs, and where that cannot be told the count is taken
// as days - which keeps more than asked rather than less, and never silently
// throws a backup away.
func retentionDays(schedule string, keep int) int {
	if keep <= 0 {
		return 30
	}
	fields := strings.Fields(schedule)
	if len(fields) >= 2 && fields[1] == "*" {
		return maxInt(1, keep/24) // hourly or more often
	}
	return keep
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// backupPrefix is where a schedule writes, kept so restoring from the old
// platform's copies still finds them.
func backupPrefix(r Row, fallback string) string {
	if p := r.Str("prefix"); p != "" {
		return strings.Trim(p, "/")
	}
	return fmt.Sprintf("%s-backups", fallback)
}
