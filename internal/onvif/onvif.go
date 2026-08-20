package onvif

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AlexxIT/go2rtc/internal/api"
	"github.com/AlexxIT/go2rtc/internal/app"
	"github.com/AlexxIT/go2rtc/internal/rtsp"
	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/onvif"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Mod struct {
		Enabled   *bool                    `yaml:"enabled"`   // master enable/disable toggle (default: true)
		Dev       string                   `yaml:"dev"`       // parent interface for auto MacVLAN + DHCP (e.g. eth0)
		Discovery *bool                    `yaml:"discovery"` // WS-Discovery responder (default: true)
		HTTPPort  int                      `yaml:"http_port"` // HTTP port on virtual IP (default: 80)
		RTSPPort  int                      `yaml:"rtsp_port"` // RTSP port on virtual IP (default: 8554)
		Streams   []string                 `yaml:"streams"`   // explicit list of stream names to publish as virtual ONVIF cameras
		Server    struct {
			Listen    string `yaml:"listen"`
			Discovery *bool  `yaml:"discovery"`
		} `yaml:"server"`
		Devices map[string]*DeviceConfig `yaml:"devices"` // optional per-stream overrides
		List    []*DeviceConfig          `yaml:"onvif"`   // list compatibility
	} `yaml:"onvif"`
}

func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	// 1. Standard structured onvif config
	type rawStandard struct {
		Mod struct {
			Enabled   *bool                    `yaml:"enabled"`
			Dev       string                   `yaml:"dev"`
			Discovery *bool                    `yaml:"discovery"`
			HTTPPort  int                      `yaml:"http_port"`
			RTSPPort  int                      `yaml:"rtsp_port"`
			Streams   []string                 `yaml:"streams"`
			Server    struct {
				Listen    string `yaml:"listen"`
				Discovery *bool  `yaml:"discovery"`
			} `yaml:"server"`
			Devices map[string]*DeviceConfig `yaml:"devices"`
			List    []*DeviceConfig          `yaml:"onvif"`
		} `yaml:"onvif"`
	}
	var standard rawStandard
	if err := node.Decode(&standard); err == nil && (standard.Mod.Dev != "" || standard.Mod.Enabled != nil || len(standard.Mod.Streams) > 0 || len(standard.Mod.Devices) > 0 || len(standard.Mod.List) > 0 || standard.Mod.Server.Listen != "") {
		c.Mod = standard.Mod
		return nil
	}

	// 2. List wrapper style (onvif: [ {name: ...}, ... ])
	var listWrapper struct {
		Onvif []*DeviceConfig `yaml:"onvif"`
	}
	if err := node.Decode(&listWrapper); err == nil && len(listWrapper.Onvif) > 0 {
		c.Mod.List = listWrapper.Onvif
		return nil
	}

	// 3. Direct map style (onvif: { cam1: {...}, cam2: {...} })
	var mapWrapper struct {
		Onvif map[string]*DeviceConfig `yaml:"onvif"`
	}
	if err := node.Decode(&mapWrapper); err == nil && len(mapWrapper.Onvif) > 0 {
		c.Mod.Devices = make(map[string]*DeviceConfig)
		for k, v := range mapWrapper.Onvif {
			if k == "server" {
				continue
			}
			c.Mod.Devices[k] = v
		}
		return nil
	}

	return nil
}

type DeviceConfig struct {
	Name            string                  `yaml:"name"`
	Dev             string                  `yaml:"dev"`       // Parent interface (for Linux MacVLAN)
	MAC             string                  `yaml:"mac"`       // LAA MAC address
	IPv4            string                  `yaml:"ipv4"`      // Static IPv4 address (e.g. 192.168.1.187/24)
	UUID            string                  `yaml:"uuid"`      // Unique ONVIF UUID
	Listen          string                  `yaml:"listen"`    // IP:Port or Port for dedicated device server
	RTSPPort        int                     `yaml:"rtsp_port"` // Custom RTSP port (defaults to rtsp_port or 8554)
	HTTPPort        int                     `yaml:"http_port"` // Custom HTTP port (defaults to http_port or 80)
	Stream          string                  `yaml:"stream"`    // Explicit stream name
	Manufacturer    string                  `yaml:"manufacturer"`
	Model           string                  `yaml:"model"`
	FirmwareVersion string                  `yaml:"firmware_version"`
	SerialNumber    string                  `yaml:"serial_number"`
	HardwareID      string                  `yaml:"hardware_id"`
	Profiles        map[string]*ProfileConf `yaml:"profiles"`
	HighQuality     *ProfileConf            `yaml:"highQuality"` // rtsp-to-onvif compatibility
	LowQuality      *ProfileConf            `yaml:"lowQuality"`  // rtsp-to-onvif compatibility
	Target          *struct {
		Hostname string `yaml:"hostname"`
		Ports    struct {
			RTSP     int `yaml:"rtsp"`
			Snapshot int `yaml:"snapshot"`
		} `yaml:"ports"`
	} `yaml:"target"`
	Ports *struct {
		Server   int `yaml:"server"`
		RTSP     int `yaml:"rtsp"`
		Snapshot int `yaml:"snapshot"`
	} `yaml:"ports"`
}

type ProfileConf struct {
	Stream      string  `yaml:"stream"`
	RTSP        string  `yaml:"rtsp"`
	Snapshot    string  `yaml:"snapshot"`
	Width       int     `yaml:"width"`
	Height      int     `yaml:"height"`
	Framerate   int     `yaml:"framerate"`
	Bitrate     int     `yaml:"bitrate"`
	Quality     float64 `yaml:"quality"`
	GovLength   int     `yaml:"gov_length"`
	H264Profile string  `yaml:"h264_profile"`
	Encoding    string  `yaml:"encoding"`
}

func Init() {
	log = app.GetLogger("onvif")

	streams.HandleFunc("onvif", streamOnvif)

	// ONVIF server on all suburls
	api.HandleFunc("/onvif/", onvifDeviceService)

	// ONVIF client autodiscovery
	api.HandleFunc("api/onvif", apiOnvif)

	// Register ONVIF stream resolver for RTSP DESCRIBE
	rtsp.AddStreamResolver(resolveONVIFStream)

	var cfg Config
	app.LoadConfig(&cfg)

	initServers(&cfg)
}

var (
	log             zerolog.Logger
	discoveryServer *onvif.DiscoveryServer
	devicesMu       sync.RWMutex
	devices         = make(map[string]*onvif.ServerDevice)
	hosts           = make(map[string]*onvif.ServerDevice)
	deviceStreams   = make(map[string]string) // hostOrIP/deviceID -> streamName
)

func initServers(cfg *Config) {
	// 1. Check if explicitly disabled
	if cfg.Mod.Enabled != nil && !*cfg.Mod.Enabled {
		log.Info().Msg("[onvif] virtual ONVIF devices disabled in config (enabled: false)")
		return
	}

	discoveryServer = onvif.NewDiscoveryServer()
	discoveryServer.OnProbe = func(remoteAddr, probeUUID string, matchedDevices int) {
		log.Debug().
			Str("remote", remoteAddr).
			Str("probe_id", probeUUID).
			Int("matched", matchedDevices).
			Msg("[onvif] ws-discovery probe received")
	}

	allDevs := make(map[string]*DeviceConfig)

	// 2. Determine which streams/devices to publish:
	if len(cfg.Mod.Streams) > 0 {
		// Option A: Explicit onvif.streams list specified
		for _, streamName := range cfg.Mod.Streams {
			if dev, exists := cfg.Mod.Devices[streamName]; exists {
				allDevs[streamName] = dev
			} else {
				allDevs[streamName] = &DeviceConfig{
					Name: streamName,
					Dev:  cfg.Mod.Dev,
				}
			}
		}
		log.Info().Int("count", len(allDevs)).Interface("selected_streams", cfg.Mod.Streams).Msg("[onvif] publishing selected streams as virtual ONVIF cameras")
	} else if len(cfg.Mod.Devices) > 0 || len(cfg.Mod.List) > 0 {
		// Option B: Explicit onvif.devices or onvif list specified
		for id, dev := range cfg.Mod.Devices {
			allDevs[id] = dev
		}
		for i, dev := range cfg.Mod.List {
			id := dev.Name
			if id == "" {
				id = "camera_" + strconv.Itoa(i)
			}
			allDevs[id] = dev
		}
		log.Info().Int("count", len(allDevs)).Msg("[onvif] publishing configured ONVIF devices")
	} else {
		// Neither onvif.streams nor onvif.devices provided: publish nothing
		log.Info().Msg("[onvif] no streams or devices specified in onvif config; no virtual ONVIF cameras published")
		return
	}

	// 3. Setup each 1:1 stream virtual camera
	idx := 0
	for id, devConf := range allDevs {
		setupDevice(id, devConf, cfg, idx)
		idx++
	}

	discoveryEnabled := true
	if cfg.Mod.Discovery != nil {
		discoveryEnabled = *cfg.Mod.Discovery
	} else if cfg.Mod.Server.Discovery != nil {
		discoveryEnabled = *cfg.Mod.Server.Discovery
	}

	if discoveryEnabled {
		go func() {
			if err := discoveryServer.Start(nil); err != nil {
				log.Warn().Err(err).Msg("[onvif] ws-discovery listener failed to start on 239.255.255.250:3702")
			} else {
				log.Info().Msg("[onvif] ws-discovery responder active on 239.255.255.250:3702")
			}
		}()
	}
}

func setupDevice(id string, conf *DeviceConfig, globalCfg *Config, idx int) {
	streamName := id
	if conf.Stream != "" {
		streamName = conf.Stream
	}
	if conf.Name == "" {
		conf.Name = id
	}
	if conf.Dev == "" && globalCfg.Mod.Dev != "" {
		conf.Dev = globalCfg.Mod.Dev
	}

	log.Info().Str("device", id).Str("stream", streamName).Msg("[onvif] initializing 1:1 virtual ONVIF device...")

	if conf.UUID == "" {
		conf.UUID = onvif.UUID()
		_ = app.PatchConfig([]string{"onvif", "devices", id, "uuid"}, conf.UUID)
	}

	// 1. MacVLAN Provisioning (starts with go2rtc_onvif_ and <= 15 chars)
	if conf.Dev != "" {
		if conf.MAC == "" {
			conf.MAC = onvif.GenerateNetworkMAC()
			log.Info().Str("device", id).Str("mac", conf.MAC).Msg("[onvif] generated LAA MAC address")
			_ = app.PatchConfig([]string{"onvif", "devices", id, "mac"}, conf.MAC)
		}

		cleanID := strings.ReplaceAll(strings.ReplaceAll(id, " ", "_"), "/", "_")
		candidate := "go2rtc_onvif_" + cleanID
		var vlanName string
		if len(candidate) <= 15 {
			vlanName = candidate
		} else {
			vlanName = fmt.Sprintf("go2rtc_onvif_%d", idx)
		}

		log.Info().
			Str("device", id).
			Str("parent_dev", conf.Dev).
			Str("vlan_name", vlanName).
			Str("mac", conf.MAC).
			Str("static_ip", conf.IPv4).
			Msg("[onvif] configuring MacVLAN interface...")

		res, err := onvif.SetupMacVLAN(conf.Dev, vlanName, conf.MAC, conf.IPv4)
		if err != nil {
			log.Error().Err(err).
				Str("device", id).
				Str("dev", conf.Dev).
				Str("vlan", vlanName).
				Str("mac", conf.MAC).
				Msg("[onvif] MacVLAN setup error")
		} else {
			log.Info().
				Str("device", id).
				Str("vlan", vlanName).
				Bool("created", res.Created).
				Str("dhcp_client", res.DHCPClient).
				Str("dhcp_output", res.DHCPOutput).
				Msg("[onvif] MacVLAN link active")
		}
	}

	// 2. Resolve Virtual IP Address
	ip := ""
	if conf.IPv4 != "" {
		ip = strings.Split(conf.IPv4, "/")[0]
		log.Info().Str("device", id).Str("ip", ip).Msg("[onvif] using configured static IPv4")
	} else if conf.MAC != "" {
		log.Info().Str("device", id).Str("mac", conf.MAC).Msg("[onvif] waiting for IP assignment from DHCP (up to 10s)...")
		var err error
		ip, err = onvif.WaitForIPv4FromMAC(conf.MAC, 10*time.Second)
		if err != nil || ip == "" {
			log.Warn().Str("device", id).Str("mac", conf.MAC).
				Msg("[onvif] DHCP lease not acquired within timeout. Available interfaces:")
			for _, ifInfo := range onvif.ListAllNetworkInterfaces() {
				log.Debug().Str("device", id).Msgf("[onvif] %s", ifInfo)
			}
		} else {
			log.Info().Str("device", id).Str("mac", conf.MAC).Str("ip", ip).Msg("[onvif] acquired IP address from DHCP")
		}
	}

	if ip == "" && conf.Target != nil && conf.Target.Hostname != "" {
		ip = conf.Target.Hostname
	}
	if ip == "" && conf.Listen != "" {
		host, _, err := net.SplitHostPort(conf.Listen)
		if err == nil && host != "" {
			ip = host
		}
	}
	if ip == "" && conf.Dev != "" {
		ip = onvif.GetInterfaceIPv4(conf.Dev)
	}
	if ip == "" {
		ip = "127.0.0.1"
	}

	// 3. HTTP and RTSP Ports
	serverPort := 80
	if conf.HTTPPort != 0 {
		serverPort = conf.HTTPPort
	} else if globalCfg.Mod.HTTPPort != 0 {
		serverPort = globalCfg.Mod.HTTPPort
	} else if conf.Ports != nil && conf.Ports.Server != 0 {
		serverPort = conf.Ports.Server
	} else if ip == "127.0.0.1" {
		serverPort = api.Port
		if serverPort == 0 {
			serverPort = 1984
		}
	}

	rtspPort := 8554
	if conf.RTSPPort != 0 {
		rtspPort = conf.RTSPPort
	} else if globalCfg.Mod.RTSPPort != 0 {
		rtspPort = globalCfg.Mod.RTSPPort
	} else if conf.Ports != nil && conf.Ports.RTSP != 0 {
		rtspPort = conf.Ports.RTSP
	} else if rtsp.Port != "" {
		if p, err := strconv.Atoi(rtsp.Port); err == nil && p != 0 {
			rtspPort = p
		}
	}

	// 4. Auto-register target source into streams if target was specified
	if conf.Target != nil && conf.Target.Hostname != "" {
		targetPort := 554
		if conf.Target.Ports.RTSP != 0 {
			targetPort = conf.Target.Ports.RTSP
		}
		targetPath := ""
		if conf.HighQuality != nil && conf.HighQuality.RTSP != "" {
			targetPath = conf.HighQuality.RTSP
		}
		targetURL := fmt.Sprintf("rtsp://%s:%d%s", conf.Target.Hostname, targetPort, targetPath)
		_, _ = streams.Patch(streamName, targetURL)
	}

	// 5. Strict 1:1 Media Profile Definition
	streamURI := fmt.Sprintf("rtsp://%s:%d/%s", ip, rtspPort, streamName)
	snapshotURI := fmt.Sprintf("http://%s:%d/api/frame.jpeg?src=%s", ip, serverPort, streamName)

	mainProfile := &onvif.MediaProfile{
		Token:       "main_stream",
		Name:        "MainStream",
		Width:       1920,
		Height:      1080,
		Framerate:   30,
		Bitrate:     4096,
		Quality:     4,
		GovLength:   30,
		H264Profile: "Main",
		Encoding:    "H264",
		StreamURI:   streamURI,
		SnapshotURI: snapshotURI,
	}

	if conf.HighQuality != nil {
		if conf.HighQuality.Width > 0 {
			mainProfile.Width = conf.HighQuality.Width
		}
		if conf.HighQuality.Height > 0 {
			mainProfile.Height = conf.HighQuality.Height
		}
		if conf.HighQuality.Framerate > 0 {
			mainProfile.Framerate = conf.HighQuality.Framerate
		}
		if conf.HighQuality.Bitrate > 0 {
			mainProfile.Bitrate = conf.HighQuality.Bitrate
		}
	}

	var profiles []*onvif.MediaProfile
	profiles = append(profiles, mainProfile)

	if conf.LowQuality != nil {
		subStreamURI := fmt.Sprintf("rtsp://%s:%d/%s", ip, rtspPort, streamName)
		subSnapshotURI := fmt.Sprintf("http://%s:%d/api/frame.jpeg?src=%s", ip, serverPort, streamName)
		subProfile := &onvif.MediaProfile{
			Token:       "sub_stream",
			Name:        "SubStream",
			Width:       conf.LowQuality.Width,
			Height:      conf.LowQuality.Height,
			Framerate:   conf.LowQuality.Framerate,
			Bitrate:     conf.LowQuality.Bitrate,
			Quality:     4,
			GovLength:   30,
			H264Profile: "Main",
			Encoding:    "H264",
			StreamURI:   subStreamURI,
			SnapshotURI: subSnapshotURI,
		}
		if subProfile.Width <= 0 {
			subProfile.Width = 640
		}
		if subProfile.Height <= 0 {
			subProfile.Height = 360
		}
		if subProfile.Framerate <= 0 {
			subProfile.Framerate = 15
		}
		if subProfile.Bitrate <= 0 {
			subProfile.Bitrate = 1024
		}
		profiles = append(profiles, subProfile)
	}

	srvDev := &onvif.ServerDevice{
		Name:            conf.Name,
		Manufacturer:    "go2rtc",
		Model:           "VirtualCamera",
		FirmwareVersion: app.Version,
		SerialNumber:    conf.SerialNumber,
		HardwareID:      conf.HardwareID,
		Profiles:        profiles,
	}

	// 6. Map device and routing
	devicesMu.Lock()
	devices[id] = srvDev
	if ip != "" && ip != "127.0.0.1" {
		hosts[ip] = srvDev
		hosts[net.JoinHostPort(ip, strconv.Itoa(serverPort))] = srvDev
		deviceStreams[ip] = streamName
		deviceStreams[net.JoinHostPort(ip, strconv.Itoa(serverPort))] = streamName
		deviceStreams[net.JoinHostPort(ip, strconv.Itoa(rtspPort))] = streamName
	}
	deviceStreams[id] = streamName
	devicesMu.Unlock()

	// 7. Register in WS-Discovery Responder
	xaddr := fmt.Sprintf("http://%s:%d/onvif/device_service", ip, serverPort)
	discoveryServer.AddDevice(&onvif.DiscoveryDeviceConfig{
		UUID:     conf.UUID,
		Name:     conf.Name,
		Hardware: "go2rtc",
		XAddr:    xaddr,
	})

	log.Info().
		Str("device", id).
		Str("stream", streamName).
		Str("ip", ip).
		Int("http_port", serverPort).
		Int("rtsp_port", rtspPort).
		Str("xaddr", xaddr).
		Msg("[onvif] 1:1 virtual ONVIF camera ready")

	// 8. Start dedicated HTTP server on Virtual IP (for SOAP & Snapshot redirect)
	if ip != "" && ip != "127.0.0.1" {
		httpAddr := net.JoinHostPort(ip, strconv.Itoa(serverPort))
		go startDeviceServer(httpAddr, srvDev, id, streamName)
	} else if conf.Listen != "" {
		go startDeviceServer(conf.Listen, srvDev, id, streamName)
	}

	// 9. Start dedicated RTSP server on Virtual IP
	if ip != "" && ip != "127.0.0.1" {
		rtspAddr := net.JoinHostPort(ip, strconv.Itoa(rtspPort))
		if _, err := rtsp.Listen(rtspAddr); err != nil {
			log.Debug().Err(err).Str("addr", rtspAddr).Str("stream", streamName).Msg("[onvif] dedicated RTSP listener (shares global port or bound)")
		} else {
			log.Info().Str("addr", rtspAddr).Str("stream", streamName).Msg("[onvif] dedicated RTSP server active on virtual interface")
		}
	}
}

func startDeviceServer(addr string, dev *onvif.ServerDevice, devID string, streamName string) {
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		// Snapshot endpoint redirect
		if r.Method == "GET" && (r.URL.Path == "/snapshot.png" || r.URL.Path == "/snapshot.jpg" || r.URL.Path == "/snapshot") {
			snapURL := fmt.Sprintf("/api/frame.jpeg?src=%s", streamName)
			log.Debug().Str("device", devID).Str("stream", streamName).Str("redirect", snapURL).Msg("[onvif] snapshot redirect")
			http.Redirect(w, r, snapURL, http.StatusTemporaryRedirect)
			return
		}

		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		op := onvif.GetRequestAction(b)
		log.Trace().Str("device", devID).Str("stream", streamName).Str("op", op).Str("remote", r.RemoteAddr).Msgf("[onvif] device SOAP request:\n%s", b)

		resp := dev.HandleRequest(b, r.Host)
		if resp == nil {
			log.Warn().Str("device", devID).Str("op", op).Msg("[onvif] unsupported SOAP operation")
			http.Error(w, "unsupported operation", http.StatusBadRequest)
			return
		}

		log.Trace().Str("device", devID).Str("op", op).Msgf("[onvif] device SOAP response:\n%s", resp)
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write(resp)
	}

	mux.HandleFunc("/", handler)
	mux.HandleFunc("/onvif/", handler)
	mux.HandleFunc("/onvif/device_service", handler)
	mux.HandleFunc("/onvif/media_service", handler)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error().Err(err).Str("addr", addr).Str("device", devID).Msg("[onvif] device HTTP server listen failed")
		return
	}

	log.Info().Str("addr", addr).Str("device", devID).Str("stream", streamName).Msg("[onvif] dedicated device HTTP server listening")
	srv := &http.Server{Handler: mux}
	_ = srv.Serve(ln)
}

func resolveONVIFStream(path string, hostOrIP string) *streams.Stream {
	devicesMu.RLock()
	defer devicesMu.RUnlock()

	cleanPath := strings.Trim(path, "/")

	// 1. Direct match by destination Host or Virtual IP
	if hostOrIP != "" {
		if streamName, ok := deviceStreams[hostOrIP]; ok {
			if s := streams.Get(streamName); s != nil {
				return s
			}
		}
		if dev, ok := hosts[hostOrIP]; ok && len(dev.Profiles) > 0 {
			if s := streams.Get(cleanPath); s != nil {
				return s
			}
			if s := streams.Get(dev.Profiles[0].Token); s != nil {
				return s
			}
			if u, err := url.Parse(dev.Profiles[0].StreamURI); err == nil {
				if s := streams.Get(strings.Trim(u.Path, "/")); s != nil {
					return s
				}
			}
		}
	}

	// 2. Direct match by device / stream ID
	if streamName, ok := deviceStreams[cleanPath]; ok {
		if s := streams.Get(streamName); s != nil {
			return s
		}
	}

	if dev, ok := devices[cleanPath]; ok && len(dev.Profiles) > 0 {
		if u, err := url.Parse(dev.Profiles[0].StreamURI); err == nil {
			if s := streams.Get(strings.Trim(u.Path, "/")); s != nil {
				return s
			}
		}
	}

	return nil
}

func findServerDevice(r *http.Request) *onvif.ServerDevice {
	devicesMu.RLock()
	defer devicesMu.RUnlock()

	// Check if path contains a specific device id e.g. /onvif/cam1/device_service
	path := strings.TrimPrefix(r.URL.Path, "/onvif/")
	if idx := strings.IndexByte(path, '/'); idx > 0 {
		id := path[:idx]
		if dev, ok := devices[id]; ok {
			return dev
		}
	}

	// Check host match
	if dev, ok := hosts[r.Host]; ok {
		return dev
	}
	hostWithoutPort, _, err := net.SplitHostPort(r.Host)
	if err == nil {
		if dev, ok := hosts[hostWithoutPort]; ok {
			return dev
		}
	}

	// If only 1 custom device registered, use it
	if len(devices) == 1 {
		for _, dev := range devices {
			return dev
		}
	}

	return nil
}

func streamOnvif(rawURL string) (core.Producer, error) {
	client, err := onvif.NewClient(rawURL)
	if err != nil {
		return nil, err
	}

	uri, err := client.GetURI()
	if err != nil {
		return nil, err
	}

	// Append hash-based arguments to the retrieved URI
	if i := strings.IndexByte(rawURL, '#'); i > 0 {
		uri += rawURL[i:]
	}

	log.Debug().Msgf("[onvif] new uri=%s", uri)

	if err = streams.Validate(uri); err != nil {
		return nil, err
	}

	return streams.GetProducer(uri)
}

func onvifDeviceService(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	operation := onvif.GetRequestAction(b)
	if operation == "" {
		http.Error(w, "malformed request body", http.StatusBadRequest)
		return
	}

	log.Trace().Msgf("[onvif] server request %s %s:\n%s", r.Method, r.RequestURI, b)

	dev := findServerDevice(r)
	if dev != nil {
		if resp := dev.HandleRequest(b, r.Host); resp != nil {
			log.Trace().Msgf("[onvif] server response:\n%s", resp)
			w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
			_, _ = w.Write(resp)
			return
		}
	}

	switch operation {
	case onvif.ServiceGetServiceCapabilities, // important for Hass
		onvif.DeviceGetNetworkInterfaces, // important for Hass
		onvif.DeviceGetSystemDateAndTime, // important for Hass
		onvif.DeviceSetSystemDateAndTime, // return just OK
		onvif.DeviceGetDiscoveryMode,
		onvif.DeviceGetDNS,
		onvif.DeviceGetHostname,
		onvif.DeviceGetNetworkDefaultGateway,
		onvif.DeviceGetNetworkProtocols,
		onvif.DeviceGetNTP,
		onvif.DeviceGetScopes,
		onvif.MediaGetVideoEncoderConfiguration,
		onvif.MediaGetVideoEncoderConfigurations,
		onvif.MediaGetAudioEncoderConfigurations,
		onvif.MediaGetVideoEncoderConfigurationOptions,
		onvif.MediaGetAudioSources,
		onvif.MediaGetAudioSourceConfigurations:
		b = onvif.StaticResponse(operation)

	case onvif.DeviceGetCapabilities:
		// important for Hass: Media section
		b = onvif.GetCapabilitiesResponse(r.Host)

	case onvif.DeviceGetServices:
		b = onvif.GetServicesResponse(r.Host)

	case onvif.DeviceGetDeviceInformation:
		// important for Hass: SerialNumber (unique server ID)
		b = onvif.GetDeviceInformationResponse("", "go2rtc", app.Version, r.Host)

	case onvif.DeviceSystemReboot:
		b = onvif.StaticResponse(operation)

		time.AfterFunc(time.Second, func() {
			os.Exit(0)
		})

	case onvif.MediaGetVideoSources:
		b = onvif.GetVideoSourcesResponse(streams.GetAllNames())

	case onvif.MediaGetProfiles:
		// important for Hass: H264 codec, width, height
		b = onvif.GetProfilesResponse(streams.GetAllNames())

	case onvif.MediaGetProfile:
		token := onvif.FindTagValue(b, "ProfileToken")
		b = onvif.GetProfileResponse(token)

	case onvif.MediaGetVideoSourceConfigurations:
		// important for Happytime Onvif Client
		b = onvif.GetVideoSourceConfigurationsResponse(streams.GetAllNames())

	case onvif.MediaGetVideoSourceConfiguration:
		token := onvif.FindTagValue(b, "ConfigurationToken")
		b = onvif.GetVideoSourceConfigurationResponse(token)

	case onvif.MediaGetStreamUri:
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host // in case of Host without port
		}

		uri := "rtsp://" + host + ":" + rtsp.Port + "/" + onvif.FindTagValue(b, "ProfileToken")
		b = onvif.GetStreamUriResponse(uri)

	case onvif.MediaGetSnapshotUri:
		uri := "http://" + r.Host + "/api/frame.jpeg?src=" + onvif.FindTagValue(b, "ProfileToken")
		b = onvif.GetSnapshotUriResponse(uri)

	default:
		http.Error(w, "unsupported operation", http.StatusBadRequest)
		log.Warn().Msgf("[onvif] unsupported operation: %s", operation)
		log.Debug().Msgf("[onvif] unsupported request:\n%s", b)
		return
	}

	log.Trace().Msgf("[onvif] server response:\n%s", b)

	w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
	if _, err = w.Write(b); err != nil {
		log.Error().Err(err).Caller().Send()
	}
}

func apiOnvif(w http.ResponseWriter, r *http.Request) {
	src := r.URL.Query().Get("src")

	var items []*api.Source

	if src == "" {
		devices, err := onvif.DiscoveryStreamingDevices()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for _, device := range devices {
			u, err := url.Parse(device.URL)
			if err != nil {
				log.Warn().Str("url", device.URL).Msg("[onvif] broken")
				continue
			}

			if u.Scheme != "http" {
				log.Warn().Str("url", device.URL).Msg("[onvif] unsupported")
				continue
			}

			u.Scheme = "onvif"
			u.User = url.UserPassword("user", "pass")

			if u.Path == onvif.PathDevice {
				u.Path = ""
			}

			items = append(items, &api.Source{
				Name: u.Host,
				URL:  u.String(),
				Info: device.Name + " " + device.Hardware,
			})
		}
	} else {
		client, err := onvif.NewClient(src)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if l := log.Trace(); l.Enabled() {
			b, _ := client.MediaRequest(onvif.MediaGetProfiles)
			l.Msgf("[onvif] src=%s profiles:\n%s", src, b)
		}

		name, err := client.GetName()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tokens, err := client.GetProfilesTokens()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for i, token := range tokens {
			items = append(items, &api.Source{
				Name: name + " stream" + strconv.Itoa(i),
				URL:  src + "?subtype=" + token,
			})
		}

		if len(tokens) > 0 && client.HasSnapshots() {
			items = append(items, &api.Source{
				Name: name + " snapshot",
				URL:  src + "?subtype=" + tokens[0] + "&snapshot",
			})
		}
	}

	api.ResponseSources(w, items)
}
