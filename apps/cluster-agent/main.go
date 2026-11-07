// Command meshploy-cluster-agent is the cluster agent: on a server whose
// Meshploy API runs elsewhere, it holds the cluster's credential and answers
// that API over the mesh. Every Kubernetes request is checked before it is sent on with the
// agent's credential, and the pods that reach the host are made here from
// Meshploy's own templates.
//
//	meshploy-cluster-agent init                  make the agent's certificate and token
//	meshploy-cluster-agent register -api-url <url> -token <mprov-...> -name <node> -mesh-ip <ip>
//	meshploy-cluster-agent serve -listen <mesh ip>:7443 -api <api mesh ip>
//	meshploy-cluster-agent approve-terminal 30m  let the API open a node terminal for a while
package main

import (
	"context"
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
	case "register":
		cmdRegister(os.Args[2:])
	case "serve":
		cmdServe(os.Args[2:])
	case "approve-terminal":
		cmdApprove(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: meshploy-cluster-agent init | register | serve | approve-terminal <duration>")
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

// cmdRegister registers this node with the Meshploy API as the one holding
// the cluster's server, sending the agent's certificate and token over the
// mesh with the single-use token minted for it. The node secret the API hands
// back is kept beside the identity.
func cmdRegister(args []string) {
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	dir := fs.String("dir", defaultDir, "where the agent's certificate, key and token live")
	apiURL := fs.String("api-url", "", "the Meshploy API's address on the mesh, e.g. http://100.64.0.1:4000")
	token := fs.String("token", os.Getenv("MESHPLOY_TOKEN"), "the single-use provisioning token minted for this cluster agent (or MESHPLOY_TOKEN)")
	name := fs.String("name", "", "this node's name")
	meshIP := fs.String("mesh-ip", "", "this node's mesh address")
	port := fs.Int("port", 7443, "the port the agent serves on at the mesh address")
	fs.Parse(args)
	if *apiURL == "" || *token == "" || *name == "" || *meshIP == "" {
		log.Fatal("register needs -api-url, -token, -name and -mesh-ip")
	}
	id := agent.Identity{Dir: *dir}
	if _, err := id.Ensure(); err != nil {
		log.Fatal(err)
	}
	res, err := agent.Register(context.Background(), *apiURL, id, agent.Registration{
		Token: *token, Name: *name, MeshIP: *meshIP, Port: *port,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("registered as node %s; the API reaches this cluster through the agent at %s:%d\n", res.NodeID, *meshIP, *port)
}
