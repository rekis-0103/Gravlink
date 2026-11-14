//go:build !windows

package security

import "fmt"

const FirewallRuleName = "Gravlink LAN Access (Private)"

func CheckPrivateFirewallStatus() (bool, string) {
	return true, "non-Windows host (firewall check skipped)"
}

func PrivateFirewallCommand(port int) string {
	return fmt.Sprintf("# Allow TCP %d on LAN only via your OS firewall", port)
}
