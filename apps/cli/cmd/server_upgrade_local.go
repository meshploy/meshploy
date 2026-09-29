package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Upgrading from a local build: images and deploy files built on a
// developer's machine and copied here, for testing what has not reached
// GitHub yet (scripts/ship.sh makes and sends the bundle). Everything else
// about the upgrade is the same: staged, backed up, health-checked, rolled
// back on failure. The server then runs on the local channel until an
// ordinary upgrade (the console's, or server-upgrade --edge) takes it back to
// the published images.
//
// A bundle is a directory:
//
//	deploy/              the repository's deploy/ folder
//	images/<name>.tar.gz each a `docker save` of ghcr.io/meshploy/<name>:local
//
// An image the stack runs on the local tag that was never shipped is taken
// from edge (:main) and tagged local, so shipping only what changed is enough.

// localChannel is the image tag a local build runs on.
const localChannel = "local"

// localBuilderRepo is where a shipped builder image goes in the gateway's own
// registry, which every build node already pulls from.
const localBuilderRepo = "meshploy/builder:" + localChannel

// localServices are the compose services whose images a bundle can carry.
var localServices = map[string]string{
	"ghcr.io/meshploy/api:" + localChannel:   "api",
	"ghcr.io/meshploy/web:" + localChannel:   "web",
	"ghcr.io/meshploy/proxy:" + localChannel: "proxy",
	"ghcr.io/meshploy/caddy:" + localChannel: "caddy",
}

// stageLocalDeploy copies the bundle's deploy/ into the staging directory,
// the way a downloaded release is laid out there.
func stageLocalDeploy(from, staged string) error {
	src := filepath.Join(from, "deploy")
	if _, err := os.Stat(filepath.Join(src, "docker-compose.yml")); err != nil {
		return fmt.Errorf("%s is not a bundle: no deploy/docker-compose.yml", from)
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return nil
		}
		if isProtectedUpgradePath(filepath.ToSlash(rel)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dst := filepath.Join(staged, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFileKeepMode(p, dst)
	})
}

// loadLocalImages loads every image the bundle carries and names them.
func loadLocalImages(runtime, from string) ([]string, error) {
	archives, _ := filepath.Glob(filepath.Join(from, "images", "*.tar*"))
	sort.Strings(archives)
	var loaded []string
	for _, a := range archives {
		fmt.Printf("Loading %s…\n", filepath.Base(a))
		out, err := runtimeOutput("", runtime, "load", "-i", a)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", filepath.Base(a), err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if name, ok := strings.CutPrefix(strings.TrimSpace(line), "Loaded image: "); ok {
				loaded = append(loaded, strings.TrimPrefix(name, "localhost/"))
			}
		}
	}
	return loaded, nil
}

// ensureLocalImages makes sure every image the stack runs is here: one on the
// local tag that was never shipped is taken from edge and tagged local; the
// rest are pulled as usual.
func ensureLocalImages(dir, runtime string) error {
	images, err := composeImages(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		return err
	}
	for _, image := range images {
		repo, tag, found := strings.Cut(image, ":"+localChannel)
		if found && tag == "" {
			if _, err := runtimeOutput("", runtime, "image", "inspect", image); err == nil {
				continue
			}
			edge := repo + ":main"
			fmt.Printf("%s was not shipped: using %s\n", image, edge)
			if err := runtimeExec(runtime, "pull", "--quiet", edge); err != nil {
				return fmt.Errorf("pull %s: %w", edge, err)
			}
			if err := runtimeExec(runtime, "tag", edge, image); err != nil {
				return fmt.Errorf("tag %s: %w", image, err)
			}
			continue
		}
		if err := runtimeExec(runtime, "pull", "--quiet", image); err != nil {
			return fmt.Errorf("pull %s: %w", image, err)
		}
	}
	return nil
}

// publishLocalBuilder puts a shipped builder image in the gateway's registry
// and points the API at it: build pods pull it from there over the mesh, as
// they pull the images they build. The registry is reached on this machine's
// loopback; the name the pods use is the mesh address.
func publishLocalBuilder(runtime string, loaded []string) error {
	shipped := "ghcr.io/meshploy/builder:" + localChannel
	if !containsString(loaded, shipped) {
		return nil
	}
	meshIP := readEnvVar("MESH_IP")
	if meshIP == "" {
		meshIP = "100.64.0.1"
	}
	push := "127.0.0.1:5000/" + localBuilderRepo
	if err := runtimeExec(runtime, "tag", shipped, push); err != nil {
		return err
	}
	args := []string{"push", "--quiet", push}
	if runtime == "podman" {
		args = []string{"push", "--quiet", "--tls-verify=false", push}
	}
	if err := runtimeExec(runtime, args...); err != nil {
		return fmt.Errorf("push the builder to the gateway's registry: %w", err)
	}
	fmt.Println("✔  Builder published to the gateway's registry")
	return setEnvVar("BUILDER_IMAGE", meshIP+":5000/"+localBuilderRepo)
}

// forgetLocalBuilder points the API back at the published builder when an
// ordinary upgrade replaces a local build.
func forgetLocalBuilder() {
	if strings.HasSuffix(readEnvVar("BUILDER_IMAGE"), "/"+localBuilderRepo) {
		if err := setEnvVar("BUILDER_IMAGE", ""); err != nil {
			fmt.Printf("warning: could not clear the local BUILDER_IMAGE: %v\n", err)
		}
	}
}

// localServicesToRecreate are the services running an image the bundle
// replaced. Compose keeps a container whose tag is unchanged even when the
// image under it is new, so these are recreated by name.
func localServicesToRecreate(loaded []string) []string {
	var out []string
	for _, image := range loaded {
		if svc, ok := localServices[image]; ok {
			out = append(out, svc)
		}
	}
	sort.Strings(out)
	return out
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
