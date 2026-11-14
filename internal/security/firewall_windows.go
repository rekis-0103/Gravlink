//go:build windows

package security

import (
	"fmt"
	"os/exec"
	"strings"
)

const FirewallRuleName = "Gravlink LAN Access (Private)"

// CheckPrivateFirewallStatus checks whether a Windows Firewall rule for Gravlink exists
// and whether the active network profile is set to Private (instead of Public).
func CheckPrivateFirewallStatus() (ok bool, detail string) {
	publicProfileAlias := detectPublicNetworkAlias()

	cmd := exec.Command("netsh", "advfirewall", "firewall", "show", "rule", fmt.Sprintf("name=%s", FirewallRuleName))
	out, err := cmd.CombinedOutput()
	text := string(out)
	ruleExists := err == nil && !strings.Contains(text, "No rules match")

	if publicProfileAlias != "" && !ruleExists {
		return false, fmt.Sprintf("active interface %q is set to Public network AND firewall rule is missing", publicProfileAlias)
	}
	if publicProfileAlias != "" {
		return false, fmt.Sprintf("active interface %q is set to Public network (Windows blocks inbound LAN traffic on Public networks)", publicProfileAlias)
	}
	if !ruleExists {
		return false, "no Private-network Windows Firewall rule found yet"
	}
	return true, "Private-profile Windows Firewall rule configured"
}

func detectPublicNetworkAlias() string {
	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		`Get-NetConnectionProfile | Where-Object { $_.NetworkCategory -eq 'Public' } | Select-Object -First 1 -ExpandProperty InterfaceAlias`,
	)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// PrivateFirewallCommand returns the PowerShell / netsh commands to switch Wi-Fi to Private and allow Gravlink on Private networks only.
func PrivateFirewallCommand(port int) string {
	alias := detectPublicNetworkAlias()
	ruleCmd := fmt.Sprintf(
		`netsh advfirewall firewall add rule name="%s" dir=in action=allow protocol=TCP localport=%d profile=private`,
		FirewallRuleName,
		port,
	)
	if alias != "" {
		return fmt.Sprintf("Set-NetConnectionProfile -InterfaceAlias %q -NetworkCategory Private\n  %s", alias, ruleCmd)
	}
	return ruleCmd
}
