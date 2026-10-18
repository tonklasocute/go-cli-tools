package portscout

import (
	"fmt"
	"strconv"
	"strings"
)

// MinPort and MaxPort bound valid TCP port numbers.
const (
	MinPort = 1
	MaxPort = 65535
)

// commonPorts is the curated list of ports scanned when no -p is given:
// ports that local development stacks commonly listen on.
var commonPorts = []int{
	22, 80, 443, 1433, 1521, 1883, 2181, 2375, 3000, 3306, 4200, 5000, 5173,
	5432, 5601, 5672, 6006, 6379, 6443, 8000, 8080, 8443, 8888, 9000, 9090,
	9092, 9200, 9229, 11211, 15672, 27017,
}

// services maps well-known ports to a short service name. It covers every
// port in commonPorts plus a few other well-known ones that are only shown
// when explicitly scanned.
var services = map[int]string{
	21:    "ftp",
	22:    "ssh",
	23:    "telnet",
	25:    "smtp",
	53:    "dns",
	80:    "http",
	110:   "pop3",
	143:   "imap",
	443:   "https",
	445:   "smb",
	587:   "submission",
	993:   "imaps",
	1433:  "mssql",
	1521:  "oracle",
	1883:  "mqtt",
	2181:  "zookeeper",
	2375:  "docker",
	2376:  "docker-tls",
	3000:  "dev-server",
	3001:  "dev-server-alt",
	3306:  "mysql",
	3389:  "rdp",
	4200:  "angular",
	4317:  "otlp-grpc",
	4318:  "otlp-http",
	5000:  "flask",
	5173:  "vite",
	5432:  "postgres",
	5601:  "kibana",
	5672:  "amqp",
	5900:  "vnc",
	6006:  "storybook",
	6379:  "redis",
	6443:  "kube-api",
	7687:  "neo4j",
	8000:  "http-alt",
	8080:  "http-proxy",
	8081:  "http-alt2",
	8086:  "influxdb",
	8443:  "https-alt",
	8888:  "jupyter",
	9000:  "php-fpm/minio",
	9090:  "prometheus",
	9092:  "kafka",
	9200:  "elasticsearch",
	9229:  "node-inspect",
	9300:  "elastic-transport",
	11211: "memcached",
	15672: "rabbitmq-mgmt",
	26257: "cockroachdb",
	27017: "mongodb",
}

// CommonPorts returns a copy of the curated default port list, sorted.
func CommonPorts() []int { return append([]int(nil), commonPorts...) }

// ServiceName returns the conventional service name for port, or "" when
// the port is not in the table.
func ServiceName(port int) string { return services[port] }

// ParsePorts parses a port specification such as "22,80,8000-8100" into a
// sorted, de-duplicated list. Entries are comma-separated single ports or
// inclusive ranges "lo-hi"; whitespace around entries and dashes is ignored
// and empty entries (e.g. a trailing comma) are skipped. Two aliases are
// accepted as entries: "common" (the curated default list) and "all"
// (1-65535). Every port must be within 1..65535.
func ParsePorts(spec string) ([]int, error) {
	var seen [MaxPort + 1]bool
	n := 0
	mark := func(p int) {
		if !seen[p] {
			seen[p] = true
			n++
		}
	}
	for _, raw := range strings.Split(spec, ",") {
		tok := strings.TrimSpace(raw)
		switch strings.ToLower(tok) {
		case "":
			continue
		case "common":
			for _, p := range commonPorts {
				mark(p)
			}
			continue
		case "all":
			for p := MinPort; p <= MaxPort; p++ {
				mark(p)
			}
			continue
		}
		lo, hi, err := parseEntry(tok)
		if err != nil {
			return nil, err
		}
		for p := lo; p <= hi; p++ {
			mark(p)
		}
	}
	if n == 0 {
		return nil, fmt.Errorf("port list %q is empty", spec)
	}
	out := make([]int, 0, n)
	for p := MinPort; p <= MaxPort; p++ {
		if seen[p] {
			out = append(out, p)
		}
	}
	return out, nil
}

// parseEntry parses "80" or "8000-8100" into an inclusive range.
func parseEntry(tok string) (lo, hi int, err error) {
	// A leading '-' would be a negative number, not a range separator.
	if i := strings.Index(tok[1:], "-"); i >= 0 {
		i++
		lo, err = parsePort(strings.TrimSpace(tok[:i]), tok)
		if err != nil {
			return 0, 0, err
		}
		hi, err = parsePort(strings.TrimSpace(tok[i+1:]), tok)
		if err != nil {
			return 0, 0, err
		}
		if lo > hi {
			return 0, 0, fmt.Errorf("invalid port range %q: start %d is greater than end %d", tok, lo, hi)
		}
		return lo, hi, nil
	}
	p, err := parsePort(tok, tok)
	return p, p, err
}

func parsePort(s, entry string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("invalid port range %q: missing bound", entry)
	}
	p, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q in %q: not a number", s, entry)
	}
	if p < MinPort || p > MaxPort {
		return 0, fmt.Errorf("port %d in %q is out of range (%d-%d)", p, entry, MinPort, MaxPort)
	}
	return p, nil
}
