package service

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/util/validation"
)

// stackPorts derives a stack service's ports from its compose definition, the
// way compose reads it: a port under ports: is published, so public, unless it
// is bound to a loopback address; a port under expose: is internal. The
// x-meshploy deploy.port names the primary port, and when compose does not
// list it, it is public, as every deploy.port was before compose was followed.
//
// declared is false when the definition says nothing about ports. The service
// then gets an internal port 3000 when it is created, and keeps the ports it
// has when it is updated. notes name what could not be carried over.
func stackPorts(def composetypes.ServiceConfig, deployPort int) (ports []PortInput, declared bool, notes []string) {
	byPort := map[int]int{} // port → index in ports
	add := func(port int, public bool, name, appProtocol string) {
		if i, ok := byPort[port]; ok {
			ports[i].IsPublic = ports[i].IsPublic || public
			return
		}
		byPort[port] = len(ports)
		ports = append(ports, PortInput{Name: name, Port: port, IsPublic: public, IsHTTP: speaksHTTP(port, appProtocol)})
	}

	for _, pc := range def.Ports {
		if !isTCP(pc.Protocol) {
			notes = append(notes, fmt.Sprintf("port %d/%s left out: only TCP ports are supported", pc.Target, pc.Protocol))
			continue
		}
		add(int(pc.Target), !isLoopback(pc.HostIP), pc.Name, pc.AppProtocol)
	}
	for _, e := range def.Expose {
		from, to, proto, err := parseExpose(e)
		switch {
		case err != nil:
			notes = append(notes, fmt.Sprintf("expose %q left out: %v", e, err))
		case !isTCP(proto):
			notes = append(notes, fmt.Sprintf("expose %q left out: only TCP ports are supported", e))
		default:
			for p := from; p <= to; p++ {
				add(p, false, "", "")
			}
		}
	}
	if deployPort > 0 {
		if _, listed := byPort[deployPort]; !listed {
			add(deployPort, true, "", "http")
		}
	}
	if len(ports) == 0 {
		return []PortInput{{Name: "http", Port: 3000, IsHTTP: true, IsPrimary: true}}, false, notes
	}

	primary := 0
	if deployPort > 0 {
		primary = byPort[deployPort]
	} else {
		for i, p := range ports {
			if p.IsPublic {
				primary = i
				break
			}
		}
	}
	ports[primary].IsPrimary = true
	nameStackPorts(ports)
	return ports, true, notes
}

// wellKnownTCP are the ports of common services that do not speak HTTP, so a
// port compose publishes for Postgres or Redis is not offered to routes.
var wellKnownTCP = map[int]bool{
	22: true, 25: true, 53: true, 110: true, 143: true, 465: true, 587: true, 993: true, 995: true,
	1025: true, 1433: true, 1521: true, 1883: true, 2181: true, 3306: true, 4222: true,
	5432: true, 5672: true, 6379: true, 6432: true, 7687: true, 8883: true, 9042: true,
	9092: true, 11211: true, 26257: true, 27017: true,
}

// speaksHTTP says whether a port serves plain HTTP, which is what a route can
// target: compose's app_protocol when it is given, otherwise every port but the
// well-known non-HTTP ones.
func speaksHTTP(port int, appProtocol string) bool {
	if appProtocol != "" {
		p := strings.ToLower(appProtocol)
		return p == "http" || p == "ws"
	}
	return !wellKnownTCP[port]
}

func isTCP(protocol string) bool {
	return protocol == "" || strings.EqualFold(protocol, "tcp")
}

func isLoopback(hostIP string) bool {
	h := strings.Trim(hostIP, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// parseExpose reads an expose: entry: "8080", "8080/tcp" or "8080-8085".
func parseExpose(e string) (from, to int, protocol string, err error) {
	spec, protocol, _ := strings.Cut(e, "/")
	lo, hi, isRange := strings.Cut(spec, "-")
	if from, err = strconv.Atoi(strings.TrimSpace(lo)); err != nil {
		return 0, 0, "", fmt.Errorf("not a port or port range")
	}
	to = from
	if isRange {
		if to, err = strconv.Atoi(strings.TrimSpace(hi)); err != nil {
			return 0, 0, "", fmt.Errorf("not a port or port range")
		}
	}
	if from < 1 || to > 65535 || to < from {
		return 0, 0, "", fmt.Errorf("not a port or port range")
	}
	if to-from >= 100 {
		return 0, 0, "", fmt.Errorf("a range of more than 100 ports")
	}
	return from, to, protocol, nil
}

// nameStackPorts gives each port a name, unique within the service: compose's
// own name when it is a valid Kubernetes port name, otherwise http or tcp for
// the primary port and http-<port> or tcp-<port> for the others.
func nameStackPorts(ports []PortInput) {
	used := map[string]bool{}
	for i := range ports {
		n := ports[i].Name
		if n != "" && len(validation.IsValidPortName(n)) == 0 && !used[n] {
			used[n] = true
			continue
		}
		ports[i].Name = ""
	}
	for i := range ports {
		if ports[i].Name != "" {
			continue
		}
		kind := "tcp"
		if ports[i].IsHTTP {
			kind = "http"
		}
		n := kind
		if !ports[i].IsPrimary || used[n] {
			n = fmt.Sprintf("%s-%d", kind, ports[i].Port)
		}
		for k := 2; used[n]; k++ {
			n = fmt.Sprintf("%s-%d-%d", kind, ports[i].Port, k)
		}
		used[n] = true
		ports[i].Name = n
	}
}

// syncStackPorts replaces a stack service's ports with the ones its compose
// definition declares. A public port that stays keeps its assigned NodePort,
// so a re-apply does not move it.
func (s *StackService) syncStackPorts(ctx context.Context, serviceID uuid.UUID, ports []PortInput) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current []meshdb.ServicePort
		if err := tx.Where("service_id = ?", serviceID).Find(&current).Error; err != nil {
			return err
		}
		nodePorts := make(map[int]int, len(current))
		for _, p := range current {
			nodePorts[p.Port] = p.NodePort
		}
		if err := tx.Where("service_id = ?", serviceID).Delete(&meshdb.ServicePort{}).Error; err != nil {
			return err
		}
		for _, p := range ports {
			row := meshdb.ServicePort{
				ServiceID: serviceID,
				Name:      p.Name,
				Port:      p.Port,
				IsHTTP:    p.IsHTTP,
				IsPrimary: p.IsPrimary,
				IsPublic:  p.IsPublic,
			}
			if p.IsPublic {
				row.NodePort = nodePorts[p.Port]
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
