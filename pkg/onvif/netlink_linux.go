//go:build linux

package onvif

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// SetupMacVLANResult contains details of the macvlan setup operation for logging.
type SetupMacVLANResult struct {
	VLANName   string `json:"vlan_name"`
	Created    bool   `json:"created"`
	DHCPClient string `json:"dhcp_client"`
	DHCPOutput string `json:"dhcp_output"`
	StaticIP   string `json:"static_ip,omitempty"`
}

// CleanupMacVLANInterfaces removes virtual network interfaces matching prefixes (e.g. go2rtc_),
// EXCEPT those whose MAC address is in preservedMACs.
func CleanupMacVLANInterfaces(preservedMACs []string, prefixes ...string) []string {
	if len(prefixes) == 0 {
		prefixes = []string{"go2rtc_", "go2rtc_onvif_", "rtsp2onvif_", "go2rc_onvif_"}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var removed []string
	for _, iface := range ifaces {
		for _, prefix := range prefixes {
			if strings.HasPrefix(iface.Name, prefix) {
				// Check if this interface MAC is in preservedMACs
				preserve := false
				if len(iface.HardwareAddr) > 0 {
					for _, pMac := range preservedMACs {
						if SameMAC(iface.HardwareAddr.String(), pMac) {
							preserve = true
							break
						}
					}
				}
				if preserve {
					break
				}

				_ = exec.Command("ip", "link", "del", "dev", iface.Name).Run()
				removed = append(removed, iface.Name)
				break
			}
		}
	}
	return removed
}

// FindNextAvailableVLANName finds the first unused interface name with the given prefix.
func FindNextAvailableVLANName(prefix string) string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return prefix + "0"
	}
	existing := make(map[string]bool)
	for _, iface := range ifaces {
		existing[iface.Name] = true
	}
	for i := 0; ; i++ {
		name := fmt.Sprintf("%s%d", prefix, i)
		if !existing[name] {
			return name
		}
	}
}

// SetupMacVLAN creates a macvlan interface on Linux, configures its MAC and IP (via static or DHCP).
// It safely detects if an interface with this MAC already exists and avoids duplicate creation or redundant DHCP requests.
func SetupMacVLAN(parentDev, vlanName, macAddress, staticIP string) (*SetupMacVLANResult, error) {
	if parentDev == "" || macAddress == "" {
		return nil, fmt.Errorf("missing required macvlan parameters (parentDev=%s, mac=%s)", parentDev, macAddress)
	}

	res := &SetupMacVLANResult{
		StaticIP: staticIP,
	}

	// 1. Check if ANY network interface with this MAC address already exists
	existingIface, _ := FindInterfaceByMAC(macAddress)
	if existingIface != nil {
		res.VLANName = existingIface.Name
		res.Created = false

		// Ensure interface is UP
		_ = exec.Command("ip", "link", "set", existingIface.Name, "up").Run()
	} else {
		// Interface with this MAC does not exist; determine a unique vlanName
		if vlanName == "" {
			vlanName = FindNextAvailableVLANName("go2rtc_")
		} else {
			// If requested vlanName already in use by another interface, find next free name
			if iface, _ := net.InterfaceByName(vlanName); iface != nil {
				vlanName = FindNextAvailableVLANName("go2rtc_")
			}
		}
		res.VLANName = vlanName

		// Create macvlan interface
		cmd := exec.Command("ip", "link", "add", vlanName, "link", parentDev, "address", macAddress, "type", "macvlan", "mode", "bridge")
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("ip link add failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
		}

		// Bring interface up
		cmd = exec.Command("ip", "link", "set", vlanName, "up")
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("ip link set up failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
		}
		res.Created = true
	}

	effectiveName := res.VLANName

	// 2. Check if interface already has an IP address assigned
	existingIP := GetIPv4FromMAC(macAddress)
	if existingIP == "" {
		existingIP = GetInterfaceIPv4(effectiveName)
	}

	if staticIP != "" {
		// If static IP already configured, skip
		if existingIP == "" || !strings.HasPrefix(staticIP, existingIP) {
			cmd := exec.Command("ip", "addr", "add", staticIP, "dev", effectiveName)
			if out, err := cmd.CombinedOutput(); err != nil {
				outStr := strings.TrimSpace(string(out))
				if !strings.Contains(outStr, "File exists") {
					return res, fmt.Errorf("ip addr add failed: %w (output: %s)", err, outStr)
				}
			}
		}
		return res, nil
	}

	// If interface already has an IP from previous lease, do not run DHCP client again
	if existingIP != "" {
		res.DHCPClient = "existing_lease"
		res.DHCPOutput = fmt.Sprintf("interface already has IPv4 %s", existingIP)
		return res, nil
	}

	// 3. No IP found yet, invoke DHCP client (dhclient, udhcpc, or dhcpcd)
	var dhcpCmd *exec.Cmd
	if path, err := exec.LookPath("dhclient"); err == nil {
		res.DHCPClient = fmt.Sprintf("dhclient (%s)", path)
		pidFile := fmt.Sprintf("/run/dhclient.%s.pid", effectiveName)
		leaseFile := fmt.Sprintf("/var/lib/dhcp/dhclient.%s.leases", effectiveName)
		dhcpCmd = exec.Command("dhclient", "-v", "-pf", pidFile, "-lf", leaseFile, effectiveName)
	} else if path, err := exec.LookPath("udhcpc"); err == nil {
		res.DHCPClient = fmt.Sprintf("udhcpc (%s)", path)
		pidFile := fmt.Sprintf("/run/udhcpc.%s.pid", effectiveName)
		dhcpCmd = exec.Command("udhcpc", "-b", "-p", pidFile, "-i", effectiveName)
	} else if path, err := exec.LookPath("dhcpcd"); err == nil {
		res.DHCPClient = fmt.Sprintf("dhcpcd (%s)", path)
		dhcpCmd = exec.Command("dhcpcd", "-b", effectiveName)
	} else {
		res.DHCPClient = "none"
		return res, fmt.Errorf("no DHCP client found in PATH (install dhclient, udhcpc, or dhcpcd, or set static ipv4 in config)")
	}

	if dhcpCmd != nil {
		out, err := dhcpCmd.CombinedOutput()
		res.DHCPOutput = strings.TrimSpace(string(out))
		if err != nil {
			// Fallback to simple dhclient invocation if flag syntax failed on minimal distro
			if strings.HasPrefix(res.DHCPClient, "dhclient") {
				simpleCmd := exec.Command("dhclient", effectiveName)
				out2, err2 := simpleCmd.CombinedOutput()
				res.DHCPOutput = strings.TrimSpace(string(out2))
				if err2 != nil {
					return res, fmt.Errorf("dhclient failed: %w (output: %s)", err2, res.DHCPOutput)
				}
			} else {
				return res, fmt.Errorf("%s failed: %w (output: %s)", res.DHCPClient, err, res.DHCPOutput)
			}
		}
	}

	return res, nil
}

// RemoveMacVLAN removes a previously created macvlan interface on Linux.
func RemoveMacVLAN(vlanName string) error {
	if vlanName == "" {
		return nil
	}
	cmd := exec.Command("ip", "link", "del", "dev", vlanName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ip link del failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
