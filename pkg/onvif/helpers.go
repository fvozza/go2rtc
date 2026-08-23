package onvif

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/AlexxIT/go2rtc/pkg/core"
)

type DiscoveryDevice struct {
	URL      string
	Name     string
	Hardware string
}

func FindTagValue(b []byte, tag string) string {
	re := regexp.MustCompile(`(?s)<(?:\w+:)?` + tag + `\b[^>]*>([^<]+)`)
	m := re.FindSubmatch(b)
	if len(m) != 2 {
		return ""
	}
	return string(m[1])
}

// UUID - generate something like 44302cbf-0d18-4feb-79b3-33b575263da3
func UUID() string {
	s := core.RandString(32, 16)
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}

// DiscoveryStreamingDevices return list of tuple (onvif_url, name, hardware)
func DiscoveryStreamingDevices() ([]DiscoveryDevice, error) {
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}

	defer conn.Close()

	// https://www.onvif.org/wp-content/uploads/2016/12/ONVIF_Feature_Discovery_Specification_16.07.pdf
	// 5.3 Discovery Procedure:
	msg := `<?xml version="1.0" ?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
	<s:Header xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing">
		<a:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</a:Action>
		<a:MessageID>urn:uuid:` + UUID() + `</a:MessageID>
		<a:To>urn:schemas-xmlsoap-org:ws:2005:04:discovery</a:To>
	</s:Header>
	<s:Body>
		<d:Probe xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery">
			<d:Types />
			<d:Scopes />
		</d:Probe>
	</s:Body>
</s:Envelope>`

	addr := &net.UDPAddr{
		IP:   net.IP{239, 255, 255, 250},
		Port: 3702,
	}

	if _, err = conn.WriteTo([]byte(msg), addr); err != nil {
		return nil, err
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var devices []DiscoveryDevice

	b := make([]byte, 8192)
	for {
		n, addr, err := conn.ReadFromUDP(b)
		if err != nil {
			break
		}

		//log.Printf("[onvif] discovery response addr=%s:\n%s", addr, b[:n])

		// ignore printers, etc
		if !strings.Contains(string(b[:n]), "onvif") {
			continue
		}

		device := DiscoveryDevice{
			URL: FindTagValue(b[:n], "XAddrs"),
		}

		if device.URL == "" {
			continue
		}

		// fix some buggy cameras
		// <wsdd:XAddrs>http://0.0.0.0:8080/onvif/device_service</wsdd:XAddrs>
		if s, ok := strings.CutPrefix(device.URL, "http://0.0.0.0"); ok {
			device.URL = "http://" + addr.IP.String() + s
		}

		// try to find the camera name and model (hardware)
		scopes := FindTagValue(b[:n], "Scopes")
		device.Name = findScope(scopes, "onvif://www.onvif.org/name/")
		device.Hardware = findScope(scopes, "onvif://www.onvif.org/hardware/")

		devices = append(devices, device)
	}

	return devices, nil
}

func findScope(s, prefix string) string {
	s = core.Between(s, prefix, " ")
	s, _ = url.QueryUnescape(s)
	return s
}

func atoi(s string) int {
	if s == "" {
		return 0
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return i
}

func GetPosixTZ(current time.Time) string {
	// Thanks to https://github.com/Path-Variable/go-posix-time
	_, offset := current.Zone()

	if current.IsDST() {
		_, end := current.ZoneBounds()
		endPlus1 := end.Add(time.Hour * 25)
		_, offset = endPlus1.Zone()
	}

	var prefix string
	if offset < 0 {
		prefix = "GMT+"
		offset = -offset / 60
	} else {
		prefix = "GMT-"
		offset = offset / 60
	}

	return prefix + fmt.Sprintf("%02d:%02d", offset/60, offset%60)
}

// getURLPath extracts the path from a URL string, using defPath as fallback.
// Unlike GetPath, this correctly returns the actual path from the URL.
func getURLPath(rawURL, defPath string) string {
	if rawURL == "" {
		return defPath
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" {
		return defPath
	}
	return u.Path
}

func GetPath(urlOrPath, defPath string) string {
	if urlOrPath == "" || urlOrPath[0] == '/' {
		return defPath
	}
	u, err := url.Parse(urlOrPath)
	if err != nil {
		return defPath
	}
	return GetPath(u.Path, defPath)
}

// GenerateNetworkMAC generates a random Unicast LAA (Locally Administered Address) MAC address with prefix 1A:11:B0
func GenerateNetworkMAC() string {
	s := strings.ToUpper(core.RandString(6, 16))
	return fmt.Sprintf("1A:11:B0:%s:%s:%s", s[0:2], s[2:4], s[4:6])
}

// NormalizeMAC parses a MAC address string and returns its canonical colon-separated format.
func NormalizeMAC(mac string) (string, error) {
	hw, err := net.ParseMAC(strings.TrimSpace(mac))
	if err != nil {
		return "", err
	}
	return hw.String(), nil
}

// SameMAC compares two MAC addresses, returning true if they represent the same hardware address.
func SameMAC(mac1, mac2 string) bool {
	hw1, err1 := net.ParseMAC(strings.TrimSpace(mac1))
	hw2, err2 := net.ParseMAC(strings.TrimSpace(mac2))
	if err1 == nil && err2 == nil {
		return hw1.String() == hw2.String()
	}
	return strings.EqualFold(strings.TrimSpace(mac1), strings.TrimSpace(mac2))
}

// FindInterfaceByMAC searches system network interfaces for an interface with the specified MAC address.
func FindInterfaceByMAC(macAddress string) (*net.Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	targetNorm, err := NormalizeMAC(macAddress)
	targetLower := strings.ToLower(strings.TrimSpace(macAddress))
	for _, iface := range ifaces {
		if len(iface.HardwareAddr) == 0 {
			continue
		}
		if err == nil {
			if ifaceNorm, errNorm := NormalizeMAC(iface.HardwareAddr.String()); errNorm == nil && ifaceNorm == targetNorm {
				return &iface, nil
			}
		}
		if strings.ToLower(iface.HardwareAddr.String()) == targetLower {
			return &iface, nil
		}
	}
	return nil, nil
}

// GetIPv4FromMAC finds the IPv4 address assigned to the network interface with the given MAC address.
func GetIPv4FromMAC(macAddress string) string {
	iface, err := FindInterfaceByMAC(macAddress)
	if err != nil || iface == nil {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return ""
}

// WaitForIPv4FromMAC polls network interfaces until an IPv4 address is assigned to the given MAC, or timeout expires.
func WaitForIPv4FromMAC(macAddress string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		ip := GetIPv4FromMAC(macAddress)
		if ip != "" {
			return ip, nil
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", fmt.Errorf("timeout waiting for DHCP IPv4 on MAC %s", macAddress)
}

// ListAllNetworkInterfaces returns a list of human-readable summaries for all system network interfaces.
func ListAllNetworkInterfaces() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return []string{fmt.Sprintf("error reading interfaces: %v", err)}
	}
	var res []string
	for _, iface := range ifaces {
		var ips []string
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ips = append(ips, addr.String())
		}
		res = append(res, fmt.Sprintf("%s (flags=%v, mac=%s, addrs=%v)", iface.Name, iface.Flags, iface.HardwareAddr, ips))
	}
	return res
}

// GetInterfaceIPv4 returns the first non-loopback IPv4 address for a named network interface.
func GetInterfaceIPv4(ifaceName string) string {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return ""
}



