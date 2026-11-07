// Command meshploy-cluster-agent is the cluster agent: on a server whose
// Meshploy API runs elsewhere, it holds the cluster's credential and answers
// that API over the mesh. Every Kubernetes request is checked before it is sent on with the
// agent's credential, and the pods that reach the host are made here from
// Meshploy's own templates.
//
//	meshploy-cluster-agent init                  make the agent's certificate and token
//	meshploy-cluster-agent serve -listen <mesh ip>:7443 -api <api mesh ip>
//	meshploy-cluster-agent approve-terminal 30m  let the API open a node terminal for a while
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/meshploy/apps/cluster-agent/agent"
	appk8s "github.com/meshploy/packages/server/k8s"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	defaultDir      = "/var/lib/meshploy/cluster-agent"
	defaultApproval = "/var/lib/meshploy/cluster-agent/terminal-approved-until"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "init":
		cmdInit(os.Args[2:])
	case "serve":
		cmdServe(os.Args[2:])
	case "approve-terminal":
		cmdApprove(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: meshploy-cluster-agent init | serve | approve-terminal <duration>")
	os.Exit(2)
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	dir := fs.String("dir", defaultDir, "where the agent's certificate, key and token live")
	fs.Parse(args)
	id := agent.Identity{Dir: *dir}
	created, err := id.Ensure()
	if err != nil {
		log.Fatal(err)
	}
	if created {
		fmt.Printf("made the cluster agent's identity in %s\n", *dir)
	} else {
		fmt.Printf("the cluster agent's identity in %s is already there\n", *dir)
	}
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dir := fs.String("dir", defaultDir, "where the agent's certificate, key and token live")
	listen := fs.String("listen", "", "the mesh address and port to serve the API on")
	from := fs.String("api", "", "the Meshploy API's mesh addresses, comma-separated: the only callers answered")
	kubeconfig := fs.String("kubeconfig", "/etc/rancher/k3s/k3s.yaml", "the cluster's admin kubeconfig")
	approval := fs.String("approval", defaultApproval, "the file holding the owner's terminal approval")
	builder := fs.String("builder-image", "", "the builder image builds run; empty refuses builds")
	fs.Parse(args)
	if *listen == "" || *from == "" {
		log.Fatal("serve needs -listen and -api")
	}

	id := agent.Identity{Dir: *dir}
	cert, err := id.TLSCertificate()
	if err != nil {
		log.Fatalf("certificate: %v (run meshploy-cluster-agent init)", err)
	}
	token, err := id.Token()
	if err != nil {
		log.Fatalf("token: %v (run meshploy-cluster-agent init)", err)
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatalf("kubeconfig: %v", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatal(err)
	}
	appk8s.RegisterJobMutator(agent.RootlessBuilds{})
	a, err := agent.New(cfg, agent.NewClusterFacts(cs, *approval), agent.BuildOptions{Image: *builder})
	if err != nil {
		log.Fatal(err)
	}
	var callers []string
	for _, c := range strings.Split(*from, ",") {
		if c = strings.TrimSpace(c); c != "" {
			callers = append(callers, c)
		}
	}
	srv := agent.Server(*listen, cert, agent.Gate{From: callers, Token: token, Next: a, Logf: log.Printf})
	log.Printf("serving the Meshploy API at %s, answering %s only", *listen, strings.Join(callers, ", "))
	log.Fatal(srv.ListenAndServeTLS("", ""))
}

func cmdApprove(args []string) {
	fs := flag.NewFlagSet("approve-terminal", flag.ExitOnError)
	approval := fs.String("approval", defaultApproval, "the file holding the owner's terminal approval")
	fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: meshploy-cluster-agent approve-terminal <duration, e.g. 30m> | off")
		os.Exit(2)
	}
	if fs.Arg(0) == "off" {
		if err := os.Remove(*approval); err != nil && !os.IsNotExist(err) {
			log.Fatal(err)
		}
		fmt.Println("the node terminal is closed to the Meshploy API")
		return
	}
	d, err := time.ParseDuration(fs.Arg(0))
	if err != nil || d <= 0 || d > 24*time.Hour {
		log.Fatal("give a duration up to 24h, like 30m")
	}
	until := time.Now().Add(d).UTC().Format(time.RFC3339)
	if err := os.WriteFile(*approval, []byte(until+"\n"), 0o600); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("the Meshploy API may open a node terminal until %s\n", until)
}
