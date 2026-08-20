package onvif

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServerDevice_HandleRequest(t *testing.T) {
	dev := &ServerDevice{
		Name:            "GardenCam",
		Manufacturer:    "go2rtc",
		Model:           "GardenCam HD",
		FirmwareVersion: "2.0.0",
		SerialNumber:    "GARDEN-001",
		HardwareID:      "1.00",
		Profiles: []*MediaProfile{
			{
				Token:       "main_stream",
				Name:        "MainStream",
				Width:       2560,
				Height:      1440,
				Framerate:   25,
				Bitrate:     4096,
				Quality:     4,
				GovLength:   25,
				H264Profile: "Main",
				Encoding:    "H264",
				StreamURI:   "rtsp://192.168.1.187:8554/garden_main",
				SnapshotURI: "http://192.168.1.187:8080/api/frame.jpeg?src=garden_main",
			},
			{
				Token:       "sub_stream",
				Name:        "SubStream",
				Width:       640,
				Height:      360,
				Framerate:   15,
				Bitrate:     1024,
				Quality:     2,
				GovLength:   15,
				H264Profile: "Baseline",
				Encoding:    "H264",
				StreamURI:   "rtsp://192.168.1.187:8554/garden_sub",
				SnapshotURI: "http://192.168.1.187:8080/api/frame.jpeg?src=garden_sub",
			},
		},
	}

	host := "192.168.1.187:8080"

	// 1. GetCapabilities
	t.Run("GetCapabilities", func(t *testing.T) {
		req := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><GetCapabilities xmlns="http://www.onvif.org/ver10/device/wsdl"/></s:Body></s:Envelope>`)
		resp := dev.HandleRequest(req, host)
		require.NotEmpty(t, resp)
		respStr := string(resp)
		require.Contains(t, respStr, "GetCapabilitiesResponse")
		require.Contains(t, respStr, "http://192.168.1.187:8080/onvif/device_service")
		require.Contains(t, respStr, "http://192.168.1.187:8080/onvif/media_service")
		require.Contains(t, respStr, "MaximumNumberOfProfiles>2")
	})

	// 2. GetDeviceInformation
	t.Run("GetDeviceInformation", func(t *testing.T) {
		req := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><GetDeviceInformation xmlns="http://www.onvif.org/ver10/device/wsdl"/></s:Body></s:Envelope>`)
		resp := dev.HandleRequest(req, host)
		require.NotEmpty(t, resp)
		respStr := string(resp)
		require.Contains(t, respStr, "Manufacturer>go2rtc</")
		require.Contains(t, respStr, "Model>GardenCam HD</")
		require.Contains(t, respStr, "SerialNumber>GARDEN-001</")
	})

	// 3. GetProfiles
	t.Run("GetProfiles", func(t *testing.T) {
		req := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><GetProfiles xmlns="http://www.onvif.org/ver10/media/wsdl"/></s:Body></s:Envelope>`)
		resp := dev.HandleRequest(req, host)
		require.NotEmpty(t, resp)
		respStr := string(resp)
		require.Contains(t, respStr, "token=\"main_stream\"")
		require.Contains(t, respStr, "token=\"sub_stream\"")
		require.Contains(t, respStr, "<tt:Width>2560</tt:Width>")
		require.Contains(t, respStr, "<tt:Height>1440</tt:Height>")
		require.Contains(t, respStr, "<tt:Width>640</tt:Width>")
	})

	// 4. GetStreamUri
	t.Run("GetStreamUri", func(t *testing.T) {
		reqMain := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><GetStreamUri xmlns="http://www.onvif.org/ver10/media/wsdl"><ProfileToken>main_stream</ProfileToken></GetStreamUri></s:Body></s:Envelope>`)
		respMain := dev.HandleRequest(reqMain, host)
		require.Contains(t, string(respMain), "rtsp://192.168.1.187:8554/garden_main")

		reqSub := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><GetStreamUri xmlns="http://www.onvif.org/ver10/media/wsdl"><ProfileToken>sub_stream</ProfileToken></GetStreamUri></s:Body></s:Envelope>`)
		respSub := dev.HandleRequest(reqSub, host)
		require.Contains(t, string(respSub), "rtsp://192.168.1.187:8554/garden_sub")
	})

	// 5. GetSnapshotUri
	t.Run("GetSnapshotUri", func(t *testing.T) {
		req := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><GetSnapshotUri xmlns="http://www.onvif.org/ver10/media/wsdl"><ProfileToken>main_stream</ProfileToken></GetSnapshotUri></s:Body></s:Envelope>`)
		resp := dev.HandleRequest(req, host)
		require.Contains(t, string(resp), "http://192.168.1.187:8080/api/frame.jpeg?src=garden_main")
	})

	// 6. GetSystemDateAndTime
	t.Run("GetSystemDateAndTime", func(t *testing.T) {
		req := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><GetSystemDateAndTime xmlns="http://www.onvif.org/ver10/device/wsdl"/></s:Body></s:Envelope>`)
		resp := dev.HandleRequest(req, host)
		require.NotEmpty(t, resp)
		require.True(t, strings.Contains(string(resp), "GetSystemDateAndTimeResponse"))
	})
}
