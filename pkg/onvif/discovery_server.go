package onvif

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type DiscoveryDeviceConfig struct {
	UUID     string   `json:"uuid"`
	Name     string   `json:"name"`
	Hardware string   `json:"hardware"`
	XAddr    string   `json:"xaddr"`
	Scopes   []string `json:"scopes,omitempty"`
	Types    []string `json:"types,omitempty"`
}

type DiscoveryServer struct {
	mu         sync.RWMutex
	devices    map[string]*DiscoveryDeviceConfig
	conn       *net.UDPConn
	msgNo      atomic.Uint64
	instanceID int64
	closed     chan struct{}
	OnProbe    func(remoteAddr string, probeUUID string, matchedDevices int)
}

func NewDiscoveryServer() *DiscoveryServer {
	return &DiscoveryServer{
		devices:    make(map[string]*DiscoveryDeviceConfig),
		instanceID: time.Now().Unix(),
		closed:     make(chan struct{}),
	}
}

func (s *DiscoveryServer) AddDevice(dev *DiscoveryDeviceConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if dev.UUID == "" {
		dev.UUID = UUID()
	}
	if dev.Hardware == "" {
		dev.Hardware = "go2rtc"
	}
	if len(dev.Types) == 0 {
		dev.Types = []string{"dn:NetworkVideoTransmitter"}
	}
	s.devices[dev.UUID] = dev
}

func (s *DiscoveryServer) RemoveDevice(uuid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.devices, uuid)
}

func (s *DiscoveryServer) GetDevices() []*DiscoveryDeviceConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]*DiscoveryDeviceConfig, 0, len(s.devices))
	for _, dev := range s.devices {
		res = append(res, dev)
	}
	return res
}

func (s *DiscoveryServer) Start(iface *net.Interface) error {
	addr := &net.UDPAddr{
		IP:   net.IPv4(239, 255, 255, 250),
		Port: 3702,
	}

	conn, err := net.ListenMulticastUDP("udp4", iface, addr)
	if err != nil {
		return err
	}

	s.conn = conn

	go s.serve()
	return nil
}

func (s *DiscoveryServer) Close() error {
	select {
	case <-s.closed:
		return nil
	default:
		close(s.closed)
	}

	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *DiscoveryServer) serve() {
	buf := make([]byte, 8192)

	for {
		select {
		case <-s.closed:
			return
		default:
		}

		n, remoteAddr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
				continue
			}
		}

		req := buf[:n]
		if !bytes.Contains(req, []byte("Probe")) {
			continue
		}

		resp, matched := s.HandleProbeWithCount(req)
		if s.OnProbe != nil {
			probeUUID := FindTagValue(req, "MessageID")
			s.OnProbe(remoteAddr.String(), probeUUID, matched)
		}
		if len(resp) > 0 {
			_, _ = s.conn.WriteToUDP(resp, remoteAddr)
		}
	}
}

func (s *DiscoveryServer) HandleProbe(req []byte) []byte {
	resp, _ := s.HandleProbeWithCount(req)
	return resp
}

func (s *DiscoveryServer) HandleProbeWithCount(req []byte) ([]byte, int) {
	probeUUID := FindTagValue(req, "MessageID")
	if probeUUID == "" {
		return nil, 0
	}

	// Extract probe types
	probeType := FindTagValue(req, "Types")
	probeScopes := FindTagValue(req, "Scopes")

	s.mu.RLock()
	defer s.mu.RUnlock()

	var matchingDevices []*DiscoveryDeviceConfig
	for _, dev := range s.devices {
		if matchProbe(dev, probeType, probeScopes) {
			matchingDevices = append(matchingDevices, dev)
		}
	}

	if len(matchingDevices) == 0 {
		return nil, 0
	}

	return s.BuildProbeMatches(probeUUID, matchingDevices), len(matchingDevices)
}

func matchProbe(dev *DiscoveryDeviceConfig, probeType, probeScopes string) bool {
	probeType = strings.TrimSpace(probeType)
	if probeType != "" {
		matched := false
		// Match Types: e.g. "dn:NetworkVideoTransmitter", "NetworkVideoTransmitter", "tds:Device"
		for _, t := range dev.Types {
			tClean := t
			if idx := strings.IndexByte(t, ':'); idx >= 0 {
				tClean = t[idx+1:]
			}
			if strings.Contains(probeType, t) || strings.Contains(probeType, tClean) {
				matched = true
				break
			}
		}
		if !matched && strings.Contains(probeType, "Device") {
			matched = true
		}
		if !matched {
			return false
		}
	}

	probeScopes = strings.TrimSpace(probeScopes)
	if probeScopes != "" {
		requiredScopes := strings.Fields(probeScopes)
		for _, reqScope := range requiredScopes {
			found := false
			for _, devScope := range dev.Scopes {
				if strings.EqualFold(devScope, reqScope) {
					found = true
					break
				}
			}
			// Check built-in scopes
			if !found {
				if strings.Contains(reqScope, "/name/") && strings.Contains(reqScope, dev.Name) {
					found = true
				} else if strings.Contains(reqScope, "/hardware/") && strings.Contains(reqScope, dev.Hardware) {
					found = true
				}
			}
			if !found {
				return false
			}
		}
	}

	return true
}

func (s *DiscoveryServer) BuildProbeMatches(probeUUID string, devices []*DiscoveryDeviceConfig) []byte {
	msgNo := s.msgNo.Add(1)

	var matches strings.Builder
	for _, dev := range devices {
		scopesStr := formatScopes(dev)
		typesStr := strings.Join(dev.Types, " ")
		if typesStr == "" {
			typesStr = "dn:NetworkVideoTransmitter"
		}

		matches.WriteString(fmt.Sprintf(`
			<d:ProbeMatch>
				<wsa:EndpointReference>
					<wsa:Address>urn:uuid:%s</wsa:Address>
				</wsa:EndpointReference>
				<d:Types>%s</d:Types>
				<d:Scopes>%s</d:Scopes>
				<d:XAddrs>%s</d:XAddrs>
				<d:MetadataVersion>1</d:MetadataVersion>
			</d:ProbeMatch>`,
			dev.UUID,
			typesStr,
			scopesStr,
			dev.XAddr,
		))
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
	<SOAP-ENV:Header>
		<wsa:MessageID>urn:uuid:%s</wsa:MessageID>
		<wsa:RelatesTo>%s</wsa:RelatesTo>
		<wsa:To SOAP-ENV:mustUnderstand="true">http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</wsa:To>
		<wsa:Action SOAP-ENV:mustUnderstand="true">http://schemas.xmlsoap.org/ws/2005/04/discovery/ProbeMatches</wsa:Action>
		<d:AppSequence SOAP-ENV:mustUnderstand="true" MessageNumber="%d" InstanceId="%d"/>
	</SOAP-ENV:Header>
	<SOAP-ENV:Body>
		<d:ProbeMatches>%s
		</d:ProbeMatches>
	</SOAP-ENV:Body>
</SOAP-ENV:Envelope>`,
		UUID(),
		probeUUID,
		msgNo,
		s.instanceID,
		matches.String(),
	)

	return []byte(response)
}

func formatScopes(dev *DiscoveryDeviceConfig) string {
	var scopes []string
	scopes = append(scopes,
		"onvif://www.onvif.org/type/video_encoder",
		"onvif://www.onvif.org/type/ptz",
		"onvif://www.onvif.org/type/Network_Video_Transmitter",
		"onvif://www.onvif.org/hardware/"+dev.Hardware,
		"onvif://www.onvif.org/name/"+dev.Name,
		"onvif://www.onvif.org/location/",
		"onvif://www.onvif.org/Profile/Streaming",
	)
	for _, sc := range dev.Scopes {
		if !slicesContains(scopes, sc) {
			scopes = append(scopes, sc)
		}
	}
	return strings.Join(scopes, " ")
}

func slicesContains(s []string, val string) bool {
	for _, item := range s {
		if item == val {
			return true
		}
	}
	return false
}
