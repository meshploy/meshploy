package migrate

import "testing"

func TestParseContainersReadsComposeAndSwarmLabels(t *testing.T) {
	out := `{"Names":"shop-web-1","Image":"shop-web","State":"running","Ports":"","Labels":"com.docker.compose.project=shop,com.docker.compose.service=web,desc=a,b"}
{"Names":"api.1.x","Image":"api:latest","State":"running","Ports":"","Labels":"com.docker.swarm.service.name=api"}
not json`
	list := ParseContainers(out)
	if len(list) != 2 || list[0].Project != "shop" || list[1].Service != "api" {
		t.Fatalf("got %+v", list)
	}
	if list[0].Labels["desc"] != "a,b" {
		t.Errorf("a comma inside a label value was split: %q", list[0].Labels["desc"])
	}
}

func TestParseServicesReadsReplicas(t *testing.T) {
	out := `{"Name":"api","Image":"api:latest@sha256:abc","Replicas":"1/1","Ports":""}
{"Name":"old","Image":"old:latest","Replicas":"0/0","Ports":"*:8085->80/tcp"}
{"Name":"global","Image":"x","Replicas":"1/1 (max 1 per node)","Ports":""}`
	list := ParseServices(out)
	if len(list) != 3 || list[0].Image != "api:latest" || list[1].Desired != 0 || list[2].Running != 1 {
		t.Fatalf("got %+v", list)
	}
}

func TestParseListenersAndHolders(t *testing.T) {
	out := `LISTEN 0 4096 0.0.0.0:80 0.0.0.0:* users:(("docker-proxy",pid=10,fd=7))
LISTEN 0 4096 [::]:443 [::]:* users:(("caddy",pid=11,fd=8))
LISTEN 0 4096 127.0.0.1:2019 0.0.0.0:* users:(("caddy",pid=11,fd=3))
LISTEN 0 4096 127.0.0.1:80 0.0.0.0:* users:(("nginx",pid=12,fd=3))`
	l := ParseListeners(out)
	if len(l) != 4 || l[1].Port != 443 || l[1].Process != "caddy" {
		t.Fatalf("got %+v", l)
	}
	if h := Holders(l, 80); len(h) != 1 || h[0] != "docker-proxy" {
		t.Errorf("holders of 80 = %v (loopback must not count)", h)
	}
}

func TestBindSourcesAndSharing(t *testing.T) {
	// The inspect line is name|network mode|bind sources.
	const out = "/api.1.x|bridge|/home/ubuntu/app/storage;/etc/dokploy/applications/x;\n/pgadmin|bridge|\n/vpn|host|\n"
	binds := ParseBindSources(out)
	c := Container{Name: "api.1.x", BindSources: binds["api.1.x"]}
	if len(c.BindSources) != 2 || len(binds["pgadmin"]) != 0 {
		t.Fatalf("got %v", binds)
	}
	// A container on the host's network cannot be reproduced by a Kubernetes
	// Deployment, so the mode has to survive the read.
	details := ParseInspect(out)
	if details["vpn"].NetworkMode != "host" || details["api.1.x"].NetworkMode != "bridge" {
		t.Errorf("network modes: %+v", details)
	}
	for path, want := range map[string]bool{"/home/ubuntu/app/storage": true, "/home/ubuntu/app": true, "/home/ubuntu/app/storage/img": true, "/home/ubuntu/other": false} {
		if c.MountsPath(path) != want {
			t.Errorf("MountsPath(%s) = %v", path, !want)
		}
	}
}

func TestParseStats(t *testing.T) {
	out := `{"Name":"api.1.x","MemUsage":"512.3MiB / 15.62GiB"}
{"Name":"db","MemUsage":"1.5GiB / 15.62GiB"}
{"Name":"idle","MemUsage":"0B / 0B"}`
	u := ParseStats(out)
	if u["api.1.x"] != 512 || u["db"] != 1536 || u["idle"] != 0 {
		t.Errorf("got %v", u)
	}
}

func TestParseVolumeSizesAndMeminfo(t *testing.T) {
	v := ParseVolumeSizes("12\t/var/lib/docker/volumes/small/_data\n15951\t/var/lib/docker/volumes/db1-data/_data\ngarbage\n")
	if len(v) != 2 || v[0].Name != "db1-data" || (Docker{Volumes: v}).TotalMB() != 15963 {
		t.Fatalf("got %+v", v)
	}
	total, avail := ParseMeminfo("MemTotal:       16000000 kB\nMemFree: 1 kB\nMemAvailable:    4096000 kB\n")
	if total != 15625 || avail != 4000 {
		t.Errorf("total=%d available=%d", total, avail)
	}
}
