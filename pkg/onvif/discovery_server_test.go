package onvif

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscoveryServer_HandleProbe(t *testing.T) {
	srv := NewDiscoveryServer()

	dev1 := &DiscoveryDeviceConfig{
		UUID:     "11111111-1111-1111-1111-111111111111",
		Name:     "FrontCam",
		Hardware: "go2rtc",
		XAddr:    "http://192.168.1.187:8080/onvif/device_service",
	}
	dev2 := &DiscoveryDeviceConfig{
		UUID:     "22222222-2222-2222-2222-222222222222",
		Name:     "BackCam",
		Hardware: "go2rtc",
		XAddr:    "http://192.168.1.188:8080/onvif/device_service",
	}

	srv.AddDevice(dev1)
	srv.AddDevice(dev2)

	// Standard WS-Discovery Probe
	probeXML := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
	<SOAP-ENV:Header>
		<wsa:MessageID>uuid:test-probe-12345</wsa:MessageID>
		<wsa:To>urn:schemas-xmlsoap-org:ws:2005:04:discovery</wsa:To>
		<wsa:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</wsa:Action>
	</SOAP-ENV:Header>
	<SOAP-ENV:Body>
		<d:Probe>
			<d:Types>dn:NetworkVideoTransmitter</d:Types>
			<d:Scopes />
		</d:Probe>
	</SOAP-ENV:Body>
</SOAP-ENV:Envelope>`)

	resp := srv.HandleProbe(probeXML)
	require.NotEmpty(t, resp)

	respStr := string(resp)
	require.Contains(t, respStr, "test-probe-12345") // RelatesTo
	require.Contains(t, respStr, "ProbeMatches")
	require.Contains(t, respStr, "11111111-1111-1111-1111-111111111111")
	require.Contains(t, respStr, "http://192.168.1.187:8080/onvif/device_service")
	require.Contains(t, respStr, "22222222-2222-2222-2222-222222222222")
	require.Contains(t, respStr, "http://192.168.1.188:8080/onvif/device_service")
	require.Contains(t, respStr, "onvif://www.onvif.org/name/FrontCam")
	require.Contains(t, respStr, "onvif://www.onvif.org/name/BackCam")
}

func TestDiscoveryServer_HandleScopedProbe(t *testing.T) {
	srv := NewDiscoveryServer()

	dev1 := &DiscoveryDeviceConfig{
		UUID:     "11111111-1111-1111-1111-111111111111",
		Name:     "FrontCam",
		Hardware: "go2rtc",
		XAddr:    "http://192.168.1.187:8080/onvif/device_service",
	}
	dev2 := &DiscoveryDeviceConfig{
		UUID:     "22222222-2222-2222-2222-222222222222",
		Name:     "BackCam",
		Hardware: "go2rtc",
		XAddr:    "http://192.168.1.188:8080/onvif/device_service",
	}

	srv.AddDevice(dev1)
	srv.AddDevice(dev2)

	// Probe specifically for FrontCam
	probeXML := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
	<SOAP-ENV:Header>
		<wsa:MessageID>uuid:test-probe-scoped</wsa:MessageID>
	</SOAP-ENV:Header>
	<SOAP-ENV:Body>
		<d:Probe>
			<d:Types>dn:NetworkVideoTransmitter</d:Types>
			<d:Scopes>onvif://www.onvif.org/name/FrontCam</d:Scopes>
		</d:Probe>
	</SOAP-ENV:Body>
</SOAP-ENV:Envelope>`)

	resp := srv.HandleProbe(probeXML)
	require.NotEmpty(t, resp)

	respStr := string(resp)
	require.Contains(t, respStr, "11111111-1111-1111-1111-111111111111")
	require.False(t, strings.Contains(respStr, "22222222-2222-2222-2222-222222222222"))
}

func TestGenerateNetworkMAC(t *testing.T) {
	mac := GenerateNetworkMAC()
	require.True(t, strings.HasPrefix(mac, "1A:11:B0:"))
	require.Len(t, mac, 17)
}
