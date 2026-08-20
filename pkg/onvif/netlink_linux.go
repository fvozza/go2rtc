//go:build linux

package onvif

import (
	"fmt"
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

// SetupMacVLAN creates a macvlan interface on Linux, configures its MAC and IP (via static or DHCP).
// It safely detects if the interface or MAC already exists and avoids duplicate creation or redundant DHCP requests.
func SetupMacVLAN(parentDev, vlanName, macAddress, staticIP string) (*SetupMacVLANResult, error) {
	if parentDev == "" || vlanName == "" || macAddress == "" {
		return nil, fmt.Errorf("missing required macvlan parameters (parentDev=%s, vlanName=%s, mac=%s)", parentDev, vlanName, macAddress)
	}

	res := &SetupMacVLANResult{
		VLANName: vlanName,
		StaticIP: staticIP,
	}

	// 1. Check if interface with this vlanName already exists
	out, err := exec.Command("ip", "link", "show", vlanName).CombinedOutput()
	if err == nil {
		res.Created = false
		outStr := strings.ToLower(string(out))
		// If MAC on existing interface differs, update it
		if !strings.Contains(outStr, strings.ToLower(macAddress)) {
			_ = exec.Command("ip", "link", "set", vlanName, "down").Run()
			_ = exec.Command("ip", "link", "set", vlanName, "address", macAddress).Run()
			_ = exec.Command("ip", "link", "set", vlanName, "up").Run()
		}
	} else {
		// Interface does not exist, create it
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

	// 2. Check if interface already has an IP address assigned
	existingIP := GetIPv4FromMAC(macAddress)
	if existingIP == "" {
		existingIP = GetInterfaceIPv4(vlanName)
	}

	if staticIP != "" {
		// If static IP already configured, skip
		if existingIP == "" || !strings.HasPrefix(staticIP, existingIP) {
			cmd := exec.Command("ip", "addr", "add", staticIP, "dev", vlanName)
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
		pidFile := fmt.Sprintf("/run/dhclient.%s.pid", vlanName)
		leaseFile := fmt.Sprintf("/var/lib/dhcp/dhclient.%s.leases", vlanName)
		dhcpCmd = exec.Command("dhclient", "-v", "-pf", pidFile, "-lf", leaseFile, vlanName)
	} else if path, err := exec.LookPath("udhcpc"); err == nil {
		res.DHCPClient = fmt.Sprintf("udhcpc (%s)", path)
		pidFile := fmt.Sprintf("/run/udhcpc.%s.pid", vlanName)
		dhcpCmd = exec.Command("udhcpc", "-b", "-p", pidFile, "-i", vlanName)
	} else if path, err := exec.LookPath("dhcpcd"); err == nil {
		res.DHCPClient = fmt.Sprintf("dhcpcd (%s)", path)
		dhcpCmd = exec.Command("dhcpcd", "-b", vlanName)
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
				simpleCmd := exec.Command("dhclient", vlanName)
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
