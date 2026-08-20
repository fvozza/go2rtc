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
		Server struct {
			Listen    string `yaml:"listen"`
			Discovery *bool  `yaml:"discovery"`
		} `yaml:"server"`
		Devices map[string]*DeviceConfig `yaml:"devices"`
		List    []*DeviceConfig          `yaml:"onvif"`
	} `yaml:"onvif"`
}

func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	// 1. Try decoding standard structured onvif config: { onvif: { server: ..., devices: ... } }
	type rawStandard struct {
		Mod struct {
			Server struct {
				Listen    string `yaml:"listen"`
				Discovery *bool  `yaml:"discovery"`
			} `yaml:"server"`
			Devices map[string]*DeviceConfig `yaml:"devices"`
			List    []*DeviceConfig          `yaml:"onvif"`
		} `yaml:"onvif"`
	}
	var standard rawStandard
	if err := node.Decode(&standard); err == nil && (len(standard.Mod.Devices) > 0 || len(standard.Mod.List) > 0 || standard.Mod.Server.Listen != "") {
		c.Mod = standard.Mod
		return nil
	}

	// 2. Try decoding onvif as a list of devices (rtsp-to-onvif style: onvif: [ {name: ...}, ... ])
	var listWrapper struct {
		Onvif []*DeviceConfig `yaml:"onvif"`
	}
	if err := node.Decode(&listWrapper); err == nil && len(listWrapper.Onvif) > 0 {
		c.Mod.List = listWrapper.Onvif
		return nil
	}

	// 3. Try decoding onvif as direct map of devices (homekit style: onvif: { camera1: {...}, camera2: {...} })
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
	RTSPPort        int                     `yaml:"rtsp_port"` // Custom RTSP port (defaults to rtsp.Port)
	HTTPPort        int                     `yaml:"http_port"` // Custom HTTP port (defaults to api.Port)
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
)

func initServers(cfg *Config) {
	discoveryServer = onvif.NewDiscoveryServer()
	discoveryServer.OnProbe = func(remoteAddr, probeUUID string, matchedDevices int) {
		log.Debug().
			Str("remote", remoteAddr).
			Str("probe_id", probeUUID).
			Int("matched", matchedDevices).
			Msg("[onvif] ws-discovery probe received")
	}

	allDevs := make(map[string]*DeviceConfig)
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

	for id, devConf := range allDevs {
		setupDevice(id, devConf)
	}

	discoveryEnabled := true
	if cfg.Mod.Server.Discovery != nil {
		discoveryEnabled = *cfg.Mod.Server.Discovery
	}

	if len(allDevs) == 0 {
		// Register default go2rtc device in discovery
		apiPort := api.Port
		if apiPort == 0 {
			apiPort = 1984
		}
		discoveryServer.AddDevice(&onvif.DiscoveryDeviceConfig{
			UUID:     onvif.UUID(),
			Name:     "go2rtc",
			Hardware: "go2rtc",
			XAddr:    fmt.Sprintf("http://127.0.0.1:%d/onvif/device_service", apiPort),
		})
		log.Info().Msg("[onvif] No devices configured, registered default go2rtc device in discovery")
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

func setupDevice(id string, conf *DeviceConfig) {
	log.Info().Str("device", id).Msg("[onvif] configuring device...")
	if conf.Name == "" {
		conf.Name = id
	}
	if conf.UUID == "" {
		conf.UUID = onvif.UUID()
		log.Debug().Str("device", id).Str("uuid", conf.UUID).Msg("[onvif] generated UUID")
		_ = app.PatchConfig([]string{"onvif", "devices", id, "uuid"}, conf.UUID)
	}

	if conf.Dev != "" {
		if conf.MAC == "" {
			conf.MAC = onvif.GenerateNetworkMAC()
			log.Info().Str("device", id).Str("mac", conf.MAC).Msg("[onvif] generated LAA MAC address")
			_ = app.PatchConfig([]string{"onvif", "devices", id, "mac"}, conf.MAC)
		}
		cleanID := strings.ReplaceAll(strings.ReplaceAll(id, " ", "_"), "/", "_")
		vlanName := "rtsp2onvif_" + cleanID
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
				Msg("[onvif] DHCP lease not acquired within timeout. Interface summary:")
			for _, ifInfo := range onvif.ListAllNetworkInterfaces() {
				log.Debug().Str("device", id).Msgf("[onvif] interface: %s", ifInfo)
			}
		} else {
			log.Info().Str("device", id).Str("mac", conf.MAC).Str("ip", ip).Msg("[onvif] acquired IP address from DHCP")
		}
	}

	if ip == "" && conf.Target != nil && conf.Target.Hostname != "" {
		ip = conf.Target.Hostname
		log.Debug().Str("device", id).Str("ip", ip).Msg("[onvif] using target hostname")
	}
	if ip == "" && conf.Listen != "" {
		host, _, err := net.SplitHostPort(conf.Listen)
		if err == nil && host != "" {
			ip = host
			log.Debug().Str("device", id).Str("ip", ip).Msg("[onvif] using listen host")
		}
	}
	if ip == "" && conf.Dev != "" {
		ip = onvif.GetInterfaceIPv4(conf.Dev)
		if ip != "" {
			log.Debug().Str("device", id).Str("ip", ip).Msg("[onvif] using parent interface IP")
		}
	}
	if ip == "" {
		ip = "127.0.0.1"
		log.Warn().Str("device", id).Msg("[onvif] no IP found, defaulting to 127.0.0.1")
	}

	serverPort := 0
	if conf.Ports != nil && conf.Ports.Server != 0 {
		serverPort = conf.Ports.Server
	} else if conf.HTTPPort != 0 {
		serverPort = conf.HTTPPort
	} else if conf.Listen != "" {
		_, p, err := net.SplitHostPort(conf.Listen)
		if err == nil {
			serverPort, _ = strconv.Atoi(p)
		} else {
			serverPort, _ = strconv.Atoi(conf.Listen)
		}
	}
	if serverPort == 0 {
		serverPort = api.Port
		if serverPort == 0 {
			serverPort = 1984
		}
	}

	rtspPort := rtsp.Port
	if conf.Ports != nil && conf.Ports.RTSP != 0 {
		rtspPort = strconv.Itoa(conf.Ports.RTSP)
	} else if conf.RTSPPort != 0 {
		rtspPort = strconv.Itoa(conf.RTSPPort)
	}
	if rtspPort == "" {
		rtspPort = "8554"
	}

	snapPort := strconv.Itoa(serverPort)
	if conf.Ports != nil && conf.Ports.Snapshot != 0 {
		snapPort = strconv.Itoa(conf.Ports.Snapshot)
	}

	targetHost := ""
	targetRTSPPort := 554
	if conf.Target != nil {
		targetHost = conf.Target.Hostname
		if conf.Target.Ports.RTSP != 0 {
			targetRTSPPort = conf.Target.Ports.RTSP
		}
	}

	// Auto-register stream sources and aliases into go2rtc streams registry
	if conf.HighQuality != nil {
		var src string
		if conf.HighQuality.Stream != "" {
			src = conf.HighQuality.Stream
		} else if conf.HighQuality.RTSP != "" {
			if strings.HasPrefix(conf.HighQuality.RTSP, "rtsp://") {
				src = conf.HighQuality.RTSP
			} else if targetHost != "" {
				src = fmt.Sprintf("rtsp://%s:%d%s", targetHost, targetRTSPPort, conf.HighQuality.RTSP)
			}
		}
		if src != "" {
			_, _ = streams.Patch(id, src)
			_, _ = streams.Patch(id+"_main", src)
			_, _ = streams.Patch("main_stream", src)
			if conf.HighQuality.RTSP != "" {
				clean := strings.Trim(conf.HighQuality.RTSP, "/")
				_, _ = streams.Patch(clean, src)
				_, _ = streams.Patch(clean+"/", src)
			}
			log.Info().Str("device", id).Str("source", src).Msg("[onvif] registered highQuality RTSP stream")
		}
	}

	if conf.LowQuality != nil {
		var src string
		if conf.LowQuality.Stream != "" {
			src = conf.LowQuality.Stream
		} else if conf.LowQuality.RTSP != "" {
			if strings.HasPrefix(conf.LowQuality.RTSP, "rtsp://") {
				src = conf.LowQuality.RTSP
			} else if targetHost != "" {
				src = fmt.Sprintf("rtsp://%s:%d%s", targetHost, targetRTSPPort, conf.LowQuality.RTSP)
			}
		}
		if src != "" {
			_, _ = streams.Patch(id+"_sub", src)
			_, _ = streams.Patch("sub_stream", src)
			if conf.LowQuality.RTSP != "" {
				clean := strings.Trim(conf.LowQuality.RTSP, "/")
				_, _ = streams.Patch(clean, src)
				_, _ = streams.Patch(clean+"/", src)
			}
			log.Info().Str("device", id).Str("source", src).Msg("[onvif] registered lowQuality RTSP stream")
		}
	}

	for pToken, pConf := range conf.Profiles {
		var src string
		if pConf.Stream != "" {
			src = pConf.Stream
		} else if pConf.RTSP != "" {
			if strings.HasPrefix(pConf.RTSP, "rtsp://") {
				src = pConf.RTSP
			} else if targetHost != "" {
				src = fmt.Sprintf("rtsp://%s:%d%s", targetHost, targetRTSPPort, pConf.RTSP)
			}
		}
		if src != "" {
			_, _ = streams.Patch(pToken, src)
			_, _ = streams.Patch(id+"_"+pToken, src)
			if pConf.RTSP != "" {
				clean := strings.Trim(pConf.RTSP, "/")
				_, _ = streams.Patch(clean, src)
				_, _ = streams.Patch(clean+"/", src)
			}
			log.Info().Str("device", id).Str("profile", pToken).Str("source", src).Msg("[onvif] registered profile RTSP stream")
		}
	}

	// If the device ID is already a registered stream in streams, alias default profiles to it
	if streams.Get(id) != nil {
		_, _ = streams.Patch("main_stream", id)
		_, _ = streams.Patch(id+"_main", id)
		_, _ = streams.Patch("sub_stream", id)
	}

	var profiles []*onvif.MediaProfile

	if conf.HighQuality != nil {
		p := buildProfile("main_stream", "MainStream", id, conf.HighQuality, ip, rtspPort, snapPort, conf.Target != nil)
		profiles = append(profiles, p)
	}
	if conf.LowQuality != nil {
		p := buildProfile("sub_stream", "SubStream", id, conf.LowQuality, ip, rtspPort, snapPort, conf.Target != nil)
		profiles = append(profiles, p)
	}
	for pToken, pConf := range conf.Profiles {
		pName := pToken
		p := buildProfile(pToken, pName, id, pConf, ip, rtspPort, snapPort, conf.Target != nil)
		profiles = append(profiles, p)
	}

	if len(profiles) == 0 {
		defaultProfile := &onvif.MediaProfile{
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
			StreamURI:   fmt.Sprintf("rtsp://%s:%s/%s", ip, rtspPort, id),
			SnapshotURI: fmt.Sprintf("http://%s:%s/api/frame.jpeg?src=%s", ip, snapPort, id),
		}
		profiles = append(profiles, defaultProfile)
	}

	srvDev := &onvif.ServerDevice{
		Name:            conf.Name,
		Manufacturer:    conf.Manufacturer,
		Model:           conf.Model,
		FirmwareVersion: conf.FirmwareVersion,
		SerialNumber:    conf.SerialNumber,
		HardwareID:      conf.HardwareID,
		Profiles:        profiles,
	}

	devicesMu.Lock()
	devices[id] = srvDev
	hosts[ip] = srvDev
	hosts[net.JoinHostPort(ip, strconv.Itoa(serverPort))] = srvDev
	devicesMu.Unlock()

	xaddr := fmt.Sprintf("http://%s:%d/onvif/device_service", ip, serverPort)
	discoveryServer.AddDevice(&onvif.DiscoveryDeviceConfig{
		UUID:     conf.UUID,
		Name:     conf.Name,
		Hardware: "go2rtc",
		XAddr:    xaddr,
	})

	log.Info().
		Str("device", id).
		Str("name", conf.Name).
		Str("ip", ip).
		Int("http_port", serverPort).
		Str("rtsp_port", rtspPort).
		Str("xaddr", xaddr).
		Int("profiles", len(profiles)).
		Msg("[onvif] virtual camera device ready")

	if conf.Listen != "" || (conf.Ports != nil && conf.Ports.Server != 0) {
		listenAddr := conf.Listen
		if listenAddr == "" && conf.Ports != nil && conf.Ports.Server != 0 {
			listenAddr = net.JoinHostPort(ip, strconv.Itoa(conf.Ports.Server))
		}
		go startDeviceServer(listenAddr, srvDev, id)
	}

	// Dedicated RTSP listener on virtual IP if custom port or IP specified
	if rtspPort != "" && (rtspPort != rtsp.Port || (conf.Ports != nil && conf.Ports.RTSP != 0) || conf.RTSPPort != 0) {
		if ip != "" && ip != "127.0.0.1" {
			rtspAddr := net.JoinHostPort(ip, rtspPort)
			if _, err := rtsp.Listen(rtspAddr); err != nil {
				log.Debug().Err(err).Str("addr", rtspAddr).Str("device", id).Msg("[onvif] dedicated RTSP listener (shares global port or already bound)")
			} else {
				log.Info().Str("addr", rtspAddr).Str("device", id).Msg("[onvif] dedicated RTSP listener active on virtual interface")
			}
		}
	}
}

func buildProfile(token, name, devID string, conf *ProfileConf, ip, rtspPort, snapPort string, hasTarget bool) *onvif.MediaProfile {
	p := &onvif.MediaProfile{
		Token:       token,
		Name:        name,
		Width:       conf.Width,
		Height:      conf.Height,
		Framerate:   conf.Framerate,
		Bitrate:     conf.Bitrate,
		Quality:     conf.Quality,
		GovLength:   conf.GovLength,
		H264Profile: conf.H264Profile,
		Encoding:    conf.Encoding,
	}
	if p.Width <= 0 {
		p.Width = 1920
	}
	if p.Height <= 0 {
		p.Height = 1080
	}
	if p.Framerate <= 0 {
		p.Framerate = 30
	}
	if p.Bitrate <= 0 {
		p.Bitrate = 4096
	}
	if p.GovLength <= 0 {
		p.GovLength = p.Framerate
	}
	if p.H264Profile == "" {
		p.H264Profile = "Main"
	}
	if p.Encoding == "" {
		p.Encoding = "H264"
	}

	streamName := conf.Stream
	if streamName == "" {
		if conf.RTSP != "" && !strings.HasPrefix(conf.RTSP, "rtsp://") {
			streamName = strings.TrimPrefix(conf.RTSP, "/")
		} else {
			streamName = devID
		}
	}

	if conf.RTSP != "" && strings.HasPrefix(conf.RTSP, "rtsp://") {
		p.StreamURI = conf.RTSP
	} else {
		p.StreamURI = fmt.Sprintf("rtsp://%s:%s/%s", ip, rtspPort, streamName)
	}

	if conf.Snapshot != "" && strings.HasPrefix(conf.Snapshot, "http://") {
		p.SnapshotURI = conf.Snapshot
	} else if conf.Snapshot != "" && hasTarget {
		p.SnapshotURI = fmt.Sprintf("http://%s:%s%s", ip, snapPort, conf.Snapshot)
	} else {
		p.SnapshotURI = fmt.Sprintf("http://%s:%s/api/frame.jpeg?src=%s", ip, snapPort, streamName)
	}

	return p
}

func resolveONVIFStream(path string, hostOrIP string) *streams.Stream {
	devicesMu.RLock()
	defer devicesMu.RUnlock()

	cleanPath := strings.Trim(path, "/")

	// 1. Direct match by device ID
	if dev, ok := devices[cleanPath]; ok && len(dev.Profiles) > 0 {
		return getStreamForProfile(dev.Profiles[0], cleanPath)
	}

	// 2. Match by destination Host / IP (virtual camera IP)
	if hostOrIP != "" && hostOrIP != "127.0.0.1" && hostOrIP != "0.0.0.0" {
		if dev, ok := hosts[hostOrIP]; ok {
			for _, p := range dev.Profiles {
				if strings.EqualFold(p.Token, cleanPath) || strings.EqualFold(p.Name, cleanPath) {
					return getStreamForProfile(p, cleanPath)
				}
			}
			if len(dev.Profiles) > 0 {
				return getStreamForProfile(dev.Profiles[0], cleanPath)
			}
		}
	}

	// 3. Match any profile across all devices
	for devID, dev := range devices {
		for _, p := range dev.Profiles {
			if strings.EqualFold(p.Token, cleanPath) || strings.EqualFold(p.Name, cleanPath) {
				return getStreamForProfile(p, devID)
			}
		}
	}

	return nil
}

func getStreamForProfile(p *onvif.MediaProfile, fallbackName string) *streams.Stream {
	if p == nil {
		return nil
	}
	if s := streams.Get(p.Token); s != nil {
		return s
	}
	if p.StreamURI != "" {
		if u, err := url.Parse(p.StreamURI); err == nil {
			clean := strings.Trim(u.Path, "/")
			if s := streams.Get(clean); s != nil {
				return s
			}
		}
	}
	if fallbackName != "" {
		if s := streams.Get(fallbackName); s != nil {
			return s
		}
	}
	return nil
}

func startDeviceServer(addr string, dev *onvif.ServerDevice, devID string) {
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && (r.URL.Path == "/snapshot.png" || r.URL.Path == "/snapshot.jpg" || r.URL.Path == "/snapshot") {
			snapURI := dev.GetSnapshotUri("main_stream")
			log.Debug().Str("device", devID).Str("redirect", snapURI).Msg("[onvif] snapshot redirect")
			http.Redirect(w, r, snapURI, http.StatusTemporaryRedirect)
			return
		}

		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		op := onvif.GetRequestAction(b)
		log.Trace().Str("device", devID).Str("op", op).Str("remote", r.RemoteAddr).Msgf("[onvif] device SOAP request:\n%s", b)

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
		log.Error().Err(err).Str("addr", addr).Str("device", devID).Msg("[onvif] device server listen failed")
		return
	}

	log.Info().Str("addr", addr).Str("device", devID).Msg("[onvif] dedicated device HTTP server listening")
	srv := &http.Server{Handler: mux}
	_ = srv.Serve(ln)
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
