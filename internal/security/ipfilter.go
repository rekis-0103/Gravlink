package security

import (
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

var privateCIDRs = mustParseCIDRs([]string{
	"127.0.0.0/8",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"::1/128",
	"fc00::/7",
	"fe80::/10",
})

func mustParseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, ipNet, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, ipNet)
	}
	return out
}

// LocalInterfaceSubnets discovers active non-loopback interface subnets and IPv4 addresses,
// ordered so the primary default-route LAN IP (e.g., Wi-Fi) comes first and virtual adapters come last.
func LocalInterfaceSubnets() ([]*net.IPNet, []net.IP) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil
	}

	preferredIP := detectPrimaryOutboundIP()

	type candidate struct {
		ip      net.IP
		ipNet   *net.IPNet
		score   int
		ifaceNm string
	}

	var candidates []candidate
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := normalizeIP(ipNet.IP)
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || !IsPrivateOrLoopbackIP(ip) {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				mask := ipNet.Mask
				if len(mask) == 16 {
					mask = mask[12:]
				}
				netEntry := &net.IPNet{IP: v4.Mask(mask), Mask: mask}
				candidates = append(candidates, candidate{
					ip:      v4,
					ipNet:   netEntry,
					score:   scoreInterface(iface.Name, v4, preferredIP),
					ifaceNm: iface.Name,
				})
			} else {
				candidates = append(candidates, candidate{
					ipNet: ipNet,
				})
			}
		}
	}

	slices.SortStableFunc(candidates, func(a, b candidate) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return strings.Compare(a.ifaceNm, b.ifaceNm)
	})

	var nets []*net.IPNet
	var ips []net.IP
	for _, c := range candidates {
		if c.ipNet != nil {
			nets = append(nets, c.ipNet)
		}
		if c.ip != nil {
			ips = append(ips, c.ip)
		}
	}
	return nets, ips
}

// detectPrimaryOutboundIP inspects the OS routing table via a UDP dial (sends zero packets).
func detectPrimaryOutboundIP() net.IP {
	conn, err := net.Dial("udp4", "1.1.1.1:80")
	if err != nil {
		return nil
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil
	}
	return normalizeIP(addr.IP)
}

func scoreInterface(name string, ip, preferredIP net.IP) int {
	score := 0
	if preferredIP != nil && ip.Equal(preferredIP) {
		score += 100
	}
	lower := strings.ToLower(name)
	if strings.Contains(lower, "wi-fi") || strings.Contains(lower, "wifi") || strings.Contains(lower, "wlan") {
		score += 40
	}
	if isLikelyVirtualInterface(lower, ip) {
		score -= 80
	}
	return score
}

func isLikelyVirtualInterface(lowerName string, ip net.IP) bool {
	virtualTokens := []string{
		"virtualbox", "vbox", "vmware", "vethernet", "hyper-v",
		"wsl", "docker", "tailscale", "zerotier", "loopback", "tunnel", "utun", "wg",
	}
	for _, tok := range virtualTokens {
		if strings.Contains(lowerName, tok) {
			return true
		}
	}
	// VirtualBox Host-Only default subnet
	if ip != nil && strings.HasPrefix(ip.String(), "192.168.56.") {
		return true
	}
	return false
}

func normalizeIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

// ExtractClientIP extracts the IP address strictly from RemoteAddr (ignoring X-Forwarded-For).
func ExtractClientIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		host = strings.TrimSpace(remoteAddr)
	}
	if zoneIdx := strings.Index(host, "%"); zoneIdx != -1 {
		host = host[:zoneIdx]
	}
	return normalizeIP(net.ParseIP(host))
}

// IsPrivateOrLoopbackIP checks if ip falls in RFC1918, loopback, link-local, or ULA ranges.
func IsPrivateOrLoopbackIP(ip net.IP) bool {
	ip = normalizeIP(ip)
	if ip == nil {
		return false
	}
	for _, n := range privateCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// IsAllowedLANIP verifies ip is private/loopback and, when strictSubnets are provided, matches one of them.
func IsAllowedLANIP(ip net.IP, strictSubnets []*net.IPNet) bool {
	ip = normalizeIP(ip)
	if ip == nil || !IsPrivateOrLoopbackIP(ip) {
		return false
	}
	if ip.IsLoopback() || len(strictSubnets) == 0 {
		return true
	}
	for _, n := range strictSubnets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// IPFilterMiddleware rejects non-LAN clients and logs blocked attempts.
func IPFilterMiddleware(strictSubnets []*net.IPNet, audit *AuditLogger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ExtractClientIP(r.RemoteAddr)
			if !IsAllowedLANIP(ip, strictSubnets) {
				if audit != nil {
					audit.Log(AuditEvent{
						Event:    "lan_ip_blocked",
						ClientIP: r.RemoteAddr,
						Method:   r.Method,
						Path:     r.URL.Path,
						Outcome:  "denied",
						Detail:   "non-LAN client IP rejected",
					})
				}
				http.Error(w, "Forbidden: LAN access only", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ValidateSameOrigin checks Origin (if present) against r.Host to block DNS rebinding & CSWSH.
func ValidateSameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}
