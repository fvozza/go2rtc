//go:build !linux

package onvif

import "errors"

// SetupMacVLANResult contains details of the macvlan setup operation for logging.
type SetupMacVLANResult struct {
	VLANName   string `json:"vlan_name"`
	Created    bool   `json:"created"`
	DHCPClient string `json:"dhcp_client"`
	DHCPOutput string `json:"dhcp_output"`
	StaticIP   string `json:"static_ip,omitempty"`
}

// SetupMacVLAN is a stub on non-Linux platforms where MacVLAN is unsupported.
func SetupMacVLAN(parentDev, vlanName, macAddress, staticIP string) (*SetupMacVLANResult, error) {
	return nil, errors.New("macvlan is only supported on Linux")
}

// RemoveMacVLAN is a stub on non-Linux platforms.
func RemoveMacVLAN(vlanName string) error {
	return nil
}
