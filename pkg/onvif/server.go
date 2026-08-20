package onvif

import (
	"bytes"
	"regexp"
	"strings"
	"time"
)

const ServiceGetServiceCapabilities = "GetServiceCapabilities"

const (
	DeviceGetCapabilities          = "GetCapabilities"
	DeviceGetDeviceInformation     = "GetDeviceInformation"
	DeviceGetDiscoveryMode         = "GetDiscoveryMode"
	DeviceGetDNS                   = "GetDNS"
	DeviceGetHostname              = "GetHostname"
	DeviceGetNetworkDefaultGateway = "GetNetworkDefaultGateway"
	DeviceGetNetworkInterfaces     = "GetNetworkInterfaces"
	DeviceGetNetworkProtocols      = "GetNetworkProtocols"
	DeviceGetNTP                   = "GetNTP"
	DeviceGetScopes                = "GetScopes"
	DeviceGetServices              = "GetServices"
	DeviceGetSystemDateAndTime     = "GetSystemDateAndTime"
	DeviceSetSystemDateAndTime     = "SetSystemDateAndTime"
	DeviceSystemReboot             = "SystemReboot"
)

const (
	MediaGetAudioEncoderConfigurations       = "GetAudioEncoderConfigurations"
	MediaGetAudioSources                     = "GetAudioSources"
	MediaGetAudioSourceConfigurations        = "GetAudioSourceConfigurations"
	MediaGetProfile                          = "GetProfile"
	MediaGetProfiles                         = "GetProfiles"
	MediaGetSnapshotUri                      = "GetSnapshotUri"
	MediaGetStreamUri                        = "GetStreamUri"
	MediaGetVideoEncoderConfiguration        = "GetVideoEncoderConfiguration"
	MediaGetVideoEncoderConfigurations       = "GetVideoEncoderConfigurations"
	MediaGetVideoEncoderConfigurationOptions = "GetVideoEncoderConfigurationOptions"
	MediaGetVideoSources                     = "GetVideoSources"
	MediaGetVideoSourceConfiguration         = "GetVideoSourceConfiguration"
	MediaGetVideoSourceConfigurations        = "GetVideoSourceConfigurations"
)

type MediaProfile struct {
	Token       string  `json:"token"`
	Name        string  `json:"name"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Framerate   int     `json:"framerate"`
	Bitrate     int     `json:"bitrate"` // in kbps
	Quality     float64 `json:"quality"`
	GovLength   int     `json:"gov_length"`
	H264Profile string  `json:"h264_profile"` // "Main", "High", "Baseline"
	Encoding    string  `json:"encoding"`     // "H264", "H265"
	StreamURI   string  `json:"stream_uri"`
	SnapshotURI string  `json:"snapshot_uri"`
}

type ServerDevice struct {
	Name            string          `json:"name"`
	Manufacturer    string          `json:"manufacturer"`
	Model           string          `json:"model"`
	FirmwareVersion string          `json:"firmware_version"`
	SerialNumber    string          `json:"serial_number"`
	HardwareID      string          `json:"hardware_id"`
	Profiles        []*MediaProfile `json:"profiles"`
}

func (d *ServerDevice) FindProfile(token string) *MediaProfile {
	for _, p := range d.Profiles {
		if p.Token == token || p.Name == token || "vsc_"+p.Token == token || "vec_"+p.Token == token || "vsrc_"+p.Token == token {
			return p
		}
	}
	return nil
}

func (d *ServerDevice) GetProfilesResponse() []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetProfilesResponse>`)
	for _, p := range d.Profiles {
		d.appendProfile(e, "Profiles", p)
	}
	e.Append(`</trt:GetProfilesResponse>`)
	return e.Bytes()
}

func (d *ServerDevice) GetProfileResponse(token string) []byte {
	p := d.FindProfile(token)
	if p == nil && len(d.Profiles) > 0 {
		p = d.Profiles[0]
	}
	e := NewEnvelope()
	e.Append(`<trt:GetProfileResponse>`)
	if p != nil {
		d.appendProfile(e, "Profile", p)
	}
	e.Append(`</trt:GetProfileResponse>`)
	return e.Bytes()
}

func (d *ServerDevice) appendProfile(e *Envelope, tag string, p *MediaProfile) {
	width := p.Width
	if width <= 0 {
		width = 1920
	}
	height := p.Height
	if height <= 0 {
		height = 1080
	}
	fps := p.Framerate
	if fps <= 0 {
		fps = 30
	}
	bitrate := p.Bitrate
	if bitrate <= 0 {
		bitrate = 4096
	}
	gov := p.GovLength
	if gov <= 0 {
		gov = fps
	}
	encoding := p.Encoding
	if encoding == "" {
		encoding = "H264"
	}
	h264Profile := p.H264Profile
	if h264Profile == "" {
		h264Profile = "Main"
	}

	e.Appendf(`<trt:%s token="%s" fixed="true">`, tag, p.Token)
	e.Appendf(`<tt:Name>%s</tt:Name>`, p.Name)
	e.Appendf(`<tt:VideoSourceConfiguration token="vsc_%s" fixed="true">
	<tt:Name>VSC_%s</tt:Name>
	<tt:UseCount>1</tt:UseCount>
	<tt:SourceToken>vsrc_%s</tt:SourceToken>
	<tt:Bounds x="0" y="0" width="%d" height="%d"></tt:Bounds>
</tt:VideoSourceConfiguration>`, p.Token, p.Name, p.Token, width, height)

	e.Appendf(`<tt:VideoEncoderConfiguration token="vec_%s">
	<tt:Name>VEC_%s</tt:Name>
	<tt:UseCount>1</tt:UseCount>
	<tt:Encoding>%s</tt:Encoding>
	<tt:Resolution><tt:Width>%d</tt:Width><tt:Height>%d</tt:Height></tt:Resolution>
	<tt:Quality>%g</tt:Quality>
	<tt:RateControl><tt:FrameRateLimit>%d</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>%d</tt:BitrateLimit></tt:RateControl>
	<tt:H264><tt:GovLength>%d</tt:GovLength><tt:H264Profile>%s</tt:H264Profile></tt:H264>
	<tt:SessionTimeout>PT10S</tt:SessionTimeout>
</tt:VideoEncoderConfiguration>`, p.Token, p.Name, encoding, width, height, p.Quality, fps, bitrate, gov, h264Profile)

	e.Appendf(`</trt:%s>`, tag)
}

func (d *ServerDevice) GetVideoSourcesResponse() []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoSourcesResponse>`)
	for _, p := range d.Profiles {
		width := p.Width
		if width <= 0 {
			width = 1920
		}
		height := p.Height
		if height <= 0 {
			height = 1080
		}
		fps := p.Framerate
		if fps <= 0 {
			fps = 30
		}
		e.Appendf(`<trt:VideoSources token="vsrc_%s">
	<tt:Framerate>%d.000000</tt:Framerate>
	<tt:Resolution><tt:Width>%d</tt:Width><tt:Height>%d</tt:Height></tt:Resolution>
</trt:VideoSources>`, p.Token, fps, width, height)
	}
	e.Append(`</trt:GetVideoSourcesResponse>`)
	return e.Bytes()
}

func (d *ServerDevice) GetVideoSourceConfigurationsResponse() []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoSourceConfigurationsResponse>`)
	for _, p := range d.Profiles {
		width := p.Width
		if width <= 0 {
			width = 1920
		}
		height := p.Height
		if height <= 0 {
			height = 1080
		}
		e.Appendf(`<tt:Configurations token="vsc_%s" fixed="true">
	<tt:Name>VSC_%s</tt:Name>
	<tt:UseCount>1</tt:UseCount>
	<tt:SourceToken>vsrc_%s</tt:SourceToken>
	<tt:Bounds x="0" y="0" width="%d" height="%d"></tt:Bounds>
</tt:Configurations>`, p.Token, p.Name, p.Token, width, height)
	}
	e.Append(`</trt:GetVideoSourceConfigurationsResponse>`)
	return e.Bytes()
}

func (d *ServerDevice) GetVideoEncoderConfigurationsResponse() []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoEncoderConfigurationsResponse>`)
	for _, p := range d.Profiles {
		d.appendVideoEncoderConfig(e, "Configurations", p)
	}
	e.Append(`</trt:GetVideoEncoderConfigurationsResponse>`)
	return e.Bytes()
}

func (d *ServerDevice) appendVideoEncoderConfig(e *Envelope, tag string, p *MediaProfile) {
	width := p.Width
	if width <= 0 {
		width = 1920
	}
	height := p.Height
	if height <= 0 {
		height = 1080
	}
	fps := p.Framerate
	if fps <= 0 {
		fps = 30
	}
	bitrate := p.Bitrate
	if bitrate <= 0 {
		bitrate = 4096
	}
	gov := p.GovLength
	if gov <= 0 {
		gov = fps
	}
	encoding := p.Encoding
	if encoding == "" {
		encoding = "H264"
	}
	h264Profile := p.H264Profile
	if h264Profile == "" {
		h264Profile = "Main"
	}

	e.Appendf(`<tt:%s token="vec_%s">
	<tt:Name>VEC_%s</tt:Name>
	<tt:UseCount>1</tt:UseCount>
	<tt:Encoding>%s</tt:Encoding>
	<tt:Resolution><tt:Width>%d</tt:Width><tt:Height>%d</tt:Height></tt:Resolution>
	<tt:Quality>%g</tt:Quality>
	<tt:RateControl><tt:FrameRateLimit>%d</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>%d</tt:BitrateLimit></tt:RateControl>
	<tt:H264><tt:GovLength>%d</tt:GovLength><tt:H264Profile>%s</tt:H264Profile></tt:H264>
	<tt:SessionTimeout>PT10S</tt:SessionTimeout>
</tt:%s>`, tag, p.Token, p.Name, encoding, width, height, p.Quality, fps, bitrate, gov, h264Profile, tag)
}

func (d *ServerDevice) GetStreamUri(token string) string {
	p := d.FindProfile(token)
	if p != nil && p.StreamURI != "" {
		return p.StreamURI
	}
	if len(d.Profiles) > 0 {
		return d.Profiles[0].StreamURI
	}
	return ""
}

func (d *ServerDevice) GetSnapshotUri(token string) string {
	p := d.FindProfile(token)
	if p != nil && p.SnapshotURI != "" {
		return p.SnapshotURI
	}
	if len(d.Profiles) > 0 {
		return d.Profiles[0].SnapshotURI
	}
	return ""
}

func (d *ServerDevice) GetDeviceInformationResponse() []byte {
	manuf := d.Manufacturer
	if manuf == "" {
		manuf = "go2rtc"
	}
	model := d.Model
	if model == "" {
		model = d.Name
	}
	firmware := d.FirmwareVersion
	if firmware == "" {
		firmware = "1.0.0"
	}
	serial := d.SerialNumber
	if serial == "" {
		serial = strings.ReplaceAll(d.Name, " ", "_") + "-0000"
	}
	hwID := d.HardwareID
	if hwID == "" {
		hwID = "1.00"
	}
	e := NewEnvelope()
	e.Appendf(`<tds:GetDeviceInformationResponse>
	<tds:Manufacturer>%s</tds:Manufacturer>
	<tds:Model>%s</tds:Model>
	<tds:FirmwareVersion>%s</tds:FirmwareVersion>
	<tds:SerialNumber>%s</tds:SerialNumber>
	<tds:HardwareId>%s</tds:HardwareId>
</tds:GetDeviceInformationResponse>`, manuf, model, firmware, serial, hwID)
	return e.Bytes()
}

func (d *ServerDevice) GetCapabilitiesResponse(host string) []byte {
	maxProfiles := len(d.Profiles)
	if maxProfiles <= 0 {
		maxProfiles = 2
	}
	e := NewEnvelope()
	e.Appendf(`<tds:GetCapabilitiesResponse>
	<tds:Capabilities>
		<tt:Device>
			<tt:XAddr>http://%s/onvif/device_service</tt:XAddr>
			<tt:Network>
				<tt:IPFilter>false</tt:IPFilter>
				<tt:ZeroConfiguration>false</tt:ZeroConfiguration>
				<tt:IPVersion6>false</tt:IPVersion6>
				<tt:DynDNS>false</tt:DynDNS>
			</tt:Network>
			<tt:System>
				<tt:DiscoveryResolve>false</tt:DiscoveryResolve>
				<tt:DiscoveryBye>false</tt:DiscoveryBye>
				<tt:RemoteDiscovery>false</tt:RemoteDiscovery>
				<tt:SystemBackup>false</tt:SystemBackup>
				<tt:SystemLogging>false</tt:SystemLogging>
				<tt:FirmwareUpgrade>false</tt:FirmwareUpgrade>
				<tt:SupportedVersions>
					<tt:Major>2</tt:Major>
					<tt:Minor>5</tt:Minor>
				</tt:SupportedVersions>
			</tt:System>
			<tt:IO>
				<tt:InputConnectors>0</tt:InputConnectors>
				<tt:RelayOutputs>1</tt:RelayOutputs>
			</tt:IO>
			<tt:Security>
				<tt:TLS1.1>false</tt:TLS1.1>
				<tt:TLS1.2>false</tt:TLS1.2>
				<tt:OnboardKeyGeneration>false</tt:OnboardKeyGeneration>
				<tt:AccessPolicyConfig>false</tt:AccessPolicyConfig>
				<tt:X.509Token>false</tt:X.509Token>
				<tt:SAMLToken>false</tt:SAMLToken>
				<tt:KerberosToken>false</tt:KerberosToken>
				<tt:RELToken>false</tt:RELToken>
			</tt:Security>
		</tt:Device>
		<tt:Media>
			<tt:XAddr>http://%s/onvif/media_service</tt:XAddr>
			<tt:StreamingCapabilities>
				<tt:RTPMulticast>false</tt:RTPMulticast>
				<tt:RTP_TCP>true</tt:RTP_TCP>
				<tt:RTP_RTSP_TCP>true</tt:RTP_RTSP_TCP>
			</tt:StreamingCapabilities>
			<tt:Extension>
				<tt:ProfileCapabilities>
					<tt:MaximumNumberOfProfiles>%d</tt:MaximumNumberOfProfiles>
				</tt:ProfileCapabilities>
			</tt:Extension>
		</tt:Media>
	</tds:Capabilities>
</tds:GetCapabilitiesResponse>`, host, host, maxProfiles)
	return e.Bytes()
}

func (d *ServerDevice) GetScopesResponse() []byte {
	e := NewEnvelope()
	e.Append(`<tds:GetScopesResponse>`)
	e.Appendf(`<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/name/%s</tt:ScopeItem></tds:Scopes>`, d.Name)
	e.Append(`<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/location/github</tt:ScopeItem></tds:Scopes>`)
	e.Append(`<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/Profile/Streaming</tt:ScopeItem></tds:Scopes>`)
	e.Append(`<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/type/Network_Video_Transmitter</tt:ScopeItem></tds:Scopes>`)
	e.Append(`<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/type/video_encoder</tt:ScopeItem></tds:Scopes>`)
	e.Append(`<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/type/ptz</tt:ScopeItem></tds:Scopes>`)
	e.Append(`<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/hardware/onvif</tt:ScopeItem></tds:Scopes>`)
	e.Append(`</tds:GetScopesResponse>`)
	return e.Bytes()
}

func (d *ServerDevice) HandleRequest(req []byte, host string) []byte {
	return d.HandleRequestWithAction(req, host, "")
}

func (d *ServerDevice) HandleRequestWithAction(req []byte, host string, action string) []byte {
	operation := action
	if operation == "" {
		operation = GetRequestAction(req)
	}
	if operation == "" {
		return nil
	}

	switch operation {
	case ServiceGetServiceCapabilities,
		DeviceGetNetworkInterfaces,
		DeviceGetDNS,
		DeviceGetHostname,
		DeviceGetNetworkDefaultGateway,
		DeviceGetNetworkProtocols,
		DeviceGetNTP,
		DeviceGetDiscoveryMode,
		DeviceSetSystemDateAndTime,
		DeviceSystemReboot,
		MediaGetAudioEncoderConfigurations,
		MediaGetAudioSources,
		MediaGetAudioSourceConfigurations,
		MediaGetVideoEncoderConfigurationOptions,
		"GetVideoSourceConfigurationOptions",
		"GetCompatibleVideoSourceConfigurations",
		"GetCompatibleVideoEncoderConfigurations",
		"GetGuaranteedNumberOfVideoEncoderInstances",
		"GetAudioOutputConfigurations",
		"GetAudioOutputs",
		"GetMetadataConfigurations",
		"GetMetadataConfigurationOptions",
		"GetEventProperties",
		"GetUsers",
		"GetWsdlUrl",
		"GetEndpointReference":
		return StaticResponse(operation)

	case DeviceGetSystemDateAndTime:
		return GetSystemDateAndTimeResponse()

	case DeviceGetCapabilities:
		return d.GetCapabilitiesResponse(host)

	case DeviceGetServices:
		return GetServicesResponse(host)

	case DeviceGetDeviceInformation:
		return d.GetDeviceInformationResponse()

	case DeviceGetScopes:
		return d.GetScopesResponse()

	case MediaGetProfiles:
		return d.GetProfilesResponse()

	case MediaGetProfile:
		token := FindTagValue(req, "ProfileToken")
		return d.GetProfileResponse(token)

	case MediaGetVideoSources:
		return d.GetVideoSourcesResponse()

	case MediaGetVideoSourceConfigurations:
		return d.GetVideoSourceConfigurationsResponse()

	case MediaGetVideoSourceConfiguration:
		token := FindTagValue(req, "ConfigurationToken")
		p := d.FindProfile(token)
		if p == nil {
			token = FindTagValue(req, "ProfileToken")
			p = d.FindProfile(token)
		}
		if p == nil && len(d.Profiles) > 0 {
			p = d.Profiles[0]
		}
		width := 1920
		height := 1080
		name := token
		if p != nil {
			if p.Width > 0 {
				width = p.Width
			}
			if p.Height > 0 {
				height = p.Height
			}
			name = p.Name
		}
		e := NewEnvelope()
		e.Appendf(`<trt:GetVideoSourceConfigurationResponse>
	<tt:Configuration token="%s" fixed="true">
		<tt:Name>VSC_%s</tt:Name>
		<tt:UseCount>1</tt:UseCount>
		<tt:SourceToken>vsrc_%s</tt:SourceToken>
		<tt:Bounds x="0" y="0" width="%d" height="%d"></tt:Bounds>
	</tt:Configuration>
</trt:GetVideoSourceConfigurationResponse>`, token, name, token, width, height)
		return e.Bytes()

	case MediaGetVideoEncoderConfigurations:
		return d.GetVideoEncoderConfigurationsResponse()

	case MediaGetVideoEncoderConfiguration:
		token := FindTagValue(req, "ConfigurationToken")
		p := d.FindProfile(token)
		if p == nil {
			token = FindTagValue(req, "ProfileToken")
			p = d.FindProfile(token)
		}
		if p == nil && len(d.Profiles) > 0 {
			p = d.Profiles[0]
		}
		e := NewEnvelope()
		e.Append(`<trt:GetVideoEncoderConfigurationResponse>`)
		if p != nil {
			d.appendVideoEncoderConfig(e, "Configuration", p)
		}
		e.Append(`</trt:GetVideoEncoderConfigurationResponse>`)
		return e.Bytes()

	case MediaGetStreamUri:
		token := FindTagValue(req, "ProfileToken")
		uri := d.GetStreamUri(token)
		return GetStreamUriResponse(uri)

	case MediaGetSnapshotUri:
		token := FindTagValue(req, "ProfileToken")
		uri := d.GetSnapshotUri(token)
		return GetSnapshotUriResponse(uri)
	}

	return nil
}

func GetRequestAction(b []byte) string {
	// 1. Match first child tag of Body (e.g. <s:Body><tds:GetCapabilities>)
	re := regexp.MustCompile(`(?is)<\s*(?:[a-zA-Z0-9_\-]+:)?Body[^>]*>\s*<\s*(?:[a-zA-Z0-9_\-]+:)?([a-zA-Z0-9_\-]+)`)
	m := re.FindSubmatch(b)
	if len(m) == 2 && len(m[1]) > 0 {
		tag := string(m[1])
		if !strings.HasPrefix(tag, "!--") {
			return tag
		}
	}

	// 2. Fallback: match known ONVIF action element anywhere in SOAP payload
	reFallback := regexp.MustCompile(`(?is)<\s*(?:[a-zA-Z0-9_\-]+:)?(GetCapabilities|GetServiceCapabilities|GetServices|GetDeviceInformation|GetSystemDateAndTime|SetSystemDateAndTime|GetProfiles|GetProfile|GetVideoSources|GetVideoSourceConfigurations|GetVideoSourceConfiguration|GetVideoSourceConfigurationOptions|GetVideoEncoderConfigurations|GetVideoEncoderConfiguration|GetVideoEncoderConfigurationOptions|GetStreamUri|GetSnapshotUri|GetScopes|GetNetworkInterfaces|GetNetworkProtocols|GetDNS|GetHostname|GetNTP|GetDiscoveryMode|SystemReboot|GetUsers|GetWsdlUrl|GetEndpointReference|GetEventProperties|GetAudioSources|GetAudioSourceConfigurations|GetAudioEncoderConfigurations)[\s/>]`)
	if m := reFallback.FindSubmatch(b); len(m) == 2 {
		return string(m[1])
	}

	// 3. Fallback: classic regex
	reOld := regexp.MustCompile(`Body[^<]+<([^ />]+)`)
	if m := reOld.FindSubmatch(b); len(m) == 2 {
		if i := bytes.IndexByte(m[1], ':'); i > 0 {
			return string(m[1][i+1:])
		}
		return string(m[1])
	}

	return ""
}

func GetCapabilitiesResponse(host string) []byte {
	e := NewEnvelope()
	e.Appendf(`<tds:GetCapabilitiesResponse>
	<tds:Capabilities>
		<tt:Device>
			<tt:XAddr>http://%s/onvif/device_service</tt:XAddr>
		</tt:Device>
		<tt:Media>
			<tt:XAddr>http://%s/onvif/media_service</tt:XAddr>
			<tt:StreamingCapabilities>
				<tt:RTPMulticast>false</tt:RTPMulticast>
				<tt:RTP_TCP>false</tt:RTP_TCP>
				<tt:RTP_RTSP_TCP>true</tt:RTP_RTSP_TCP>
			</tt:StreamingCapabilities>
		</tt:Media>
	</tds:Capabilities>
</tds:GetCapabilitiesResponse>`, host, host)
	return e.Bytes()
}

func GetServicesResponse(host string) []byte {
	e := NewEnvelope()
	e.Appendf(`<tds:GetServicesResponse>
	<tds:Service>
		<tds:Namespace>http://www.onvif.org/ver10/device/wsdl</tds:Namespace>
		<tds:XAddr>http://%s/onvif/device_service</tds:XAddr>
		<tds:Version><tt:Major>2</tt:Major><tt:Minor>5</tt:Minor></tds:Version>
	</tds:Service>
	<tds:Service>
		<tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace>
		<tds:XAddr>http://%s/onvif/media_service</tds:XAddr>
		<tds:Version><tt:Major>2</tt:Major><tt:Minor>5</tt:Minor></tds:Version>
	</tds:Service>
</tds:GetServicesResponse>`, host, host)
	return e.Bytes()
}

func GetSystemDateAndTimeResponse() []byte {
	loc := time.Now()
	utc := loc.UTC()

	e := NewEnvelope()
	e.Appendf(`<tds:GetSystemDateAndTimeResponse>
	<tds:SystemDateAndTime>
		<tt:DateTimeType>NTP</tt:DateTimeType>
		<tt:DaylightSavings>true</tt:DaylightSavings>
		<tt:TimeZone>
			<tt:TZ>%s</tt:TZ>
		</tt:TimeZone>
		<tt:UTCDateTime>
			<tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute><tt:Second>%d</tt:Second></tt:Time>
			<tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date>
		</tt:UTCDateTime>
		<tt:LocalDateTime>
			<tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute><tt:Second>%d</tt:Second></tt:Time>
			<tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date>
		</tt:LocalDateTime>
	</tds:SystemDateAndTime>
</tds:GetSystemDateAndTimeResponse>`,
		GetPosixTZ(loc),
		utc.Hour(), utc.Minute(), utc.Second(), utc.Year(), utc.Month(), utc.Day(),
		loc.Hour(), loc.Minute(), loc.Second(), loc.Year(), loc.Month(), loc.Day(),
	)
	return e.Bytes()
}

func GetDeviceInformationResponse(manuf, model, firmware, serial string) []byte {
	e := NewEnvelope()
	e.Appendf(`<tds:GetDeviceInformationResponse>
	<tds:Manufacturer>%s</tds:Manufacturer>
	<tds:Model>%s</tds:Model>
	<tds:FirmwareVersion>%s</tds:FirmwareVersion>
	<tds:SerialNumber>%s</tds:SerialNumber>
	<tds:HardwareId>1.00</tds:HardwareId>
</tds:GetDeviceInformationResponse>`, manuf, model, firmware, serial)
	return e.Bytes()
}

func GetProfilesResponse(names []string) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetProfilesResponse>`)
	for _, name := range names {
		appendProfile(e, "Profiles", name)
	}
	e.Append(`</trt:GetProfilesResponse>`)
	return e.Bytes()
}

func GetProfileResponse(name string) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetProfileResponse>`)
	appendProfile(e, "Profile", name)
	e.Append(`</trt:GetProfileResponse>`)
	return e.Bytes()
}

func appendProfile(e *Envelope, tag, name string) {
	// go2rtc name = ONVIF Profile Name = ONVIF Profile token
	e.Appendf(`<trt:%s token="%s" fixed="true">`, tag, name)
	e.Appendf(`<tt:Name>%s</tt:Name>`, name)
	appendVideoSourceConfiguration(e, "VideoSourceConfiguration", name)
	appendVideoEncoderConfiguration(e, "VideoEncoderConfiguration")
	e.Appendf(`</trt:%s>`, tag)
}

func GetVideoSourcesResponse(names []string) []byte {
	// go2rtc name = ONVIF VideoSource token
	e := NewEnvelope()
	e.Append(`<trt:GetVideoSourcesResponse>`)
	for _, name := range names {
		e.Appendf(`<trt:VideoSources token="%s">
	<tt:Framerate>30.000000</tt:Framerate>
	<tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution>
</trt:VideoSources>`, name)
	}
	e.Append(`</trt:GetVideoSourcesResponse>`)
	return e.Bytes()
}

func GetVideoSourceConfigurationsResponse(names []string) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoSourceConfigurationsResponse>`)
	for _, name := range names {
		appendVideoSourceConfiguration(e, "Configurations", name)
	}
	e.Append(`</trt:GetVideoSourceConfigurationsResponse>`)
	return e.Bytes()
}

func GetVideoSourceConfigurationResponse(name string) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoSourceConfigurationResponse>`)
	appendVideoSourceConfiguration(e, "Configuration", name)
	e.Append(`</trt:GetVideoSourceConfigurationResponse>`)
	return e.Bytes()
}

func appendVideoSourceConfiguration(e *Envelope, tag, name string) {
	// go2rtc name = ONVIF VideoSourceConfiguration token
	e.Appendf(`<tt:%s token="%s" fixed="true">
	<tt:Name>VSC</tt:Name>
	<tt:SourceToken>%s</tt:SourceToken>
	<tt:Bounds x="0" y="0" width="1920" height="1080"></tt:Bounds>
</tt:%s>`, tag, name, name, tag)
}

func GetVideoEncoderConfigurationsResponse() []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoEncoderConfigurationsResponse>`)
	appendVideoEncoderConfiguration(e, "VideoEncoderConfigurations")
	e.Append(`</trt:GetVideoEncoderConfigurationsResponse>`)
	return e.Bytes()
}

func GetVideoEncoderConfigurationResponse() []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoEncoderConfigurationResponse>`)
	appendVideoEncoderConfiguration(e, "VideoEncoderConfiguration")
	e.Append(`</trt:GetVideoEncoderConfigurationResponse>`)
	return e.Bytes()
}

func appendVideoEncoderConfiguration(e *Envelope, tag string) {
	// empty `RateControl` important for UniFi Protect
	e.Appendf(`<tt:%s token="vec">
		<tt:Name>VEC</tt:Name>
        <tt:UseCount>1</tt:UseCount>
		<tt:Encoding>H264</tt:Encoding>
		<tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution>
        <tt:Quality>0</tt:Quality>
		<tt:RateControl><tt:FrameRateLimit>30</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>8192</tt:BitrateLimit></tt:RateControl>
        <tt:H264><tt:GovLength>10</tt:GovLength><tt:H264Profile>Main</tt:H264Profile></tt:H264>
        <tt:SessionTimeout>PT10S</tt:SessionTimeout>
	</tt:%s>`, tag, tag)
}

func GetStreamUriResponse(uri string) []byte {
	e := NewEnvelope()
	e.Appendf(`<trt:GetStreamUriResponse><trt:MediaUri><tt:Uri>%s</tt:Uri></trt:MediaUri></trt:GetStreamUriResponse>`, uri)
	return e.Bytes()
}

func GetSnapshotUriResponse(uri string) []byte {
	e := NewEnvelope()
	e.Appendf(`<trt:GetSnapshotUriResponse><trt:MediaUri><tt:Uri>%s</tt:Uri></trt:MediaUri></trt:GetSnapshotUriResponse>`, uri)
	return e.Bytes()
}

func StaticResponse(operation string) []byte {
	switch operation {
	case DeviceGetSystemDateAndTime:
		return GetSystemDateAndTimeResponse()
	case MediaGetVideoEncoderConfiguration:
		return GetVideoEncoderConfigurationResponse()
	case MediaGetVideoEncoderConfigurations:
		return GetVideoEncoderConfigurationsResponse()
	}

	e := NewEnvelope()
	e.Append(responses[operation])
	return e.Bytes()
}

var responses = map[string]string{
	ServiceGetServiceCapabilities: `<trt:GetServiceCapabilitiesResponse>
	<trt:Capabilities SnapshotUri="true" Rotation="false" VideoSourceMode="false" OSD="false" TemporaryOSDText="false" EXICompression="false">
		<trt:StreamingCapabilities RTPMulticast="false" RTP_TCP="false" RTP_RTSP_TCP="true" NonAggregateControl="false" NoRTSPStreaming="false" />
	</trt:Capabilities>
</trt:GetServiceCapabilitiesResponse>`,

	DeviceGetDiscoveryMode:         `<tds:GetDiscoveryModeResponse><tds:DiscoveryMode>Discoverable</tds:DiscoveryMode></tds:GetDiscoveryModeResponse>`,
	DeviceGetDNS:                   `<tds:GetDNSResponse><tds:DNSInformation /></tds:GetDNSResponse>`,
	DeviceGetHostname:              `<tds:GetHostnameResponse><tds:HostnameInformation /></tds:GetHostnameResponse>`,
	DeviceGetNetworkDefaultGateway: `<tds:GetNetworkDefaultGatewayResponse><tds:NetworkGateway /></tds:GetNetworkDefaultGatewayResponse>`,
	DeviceGetNTP:                   `<tds:GetNTPResponse><tds:NTPInformation /></tds:GetNTPResponse>`,
	DeviceSetSystemDateAndTime:     `<tds:SetSystemDateAndTimeResponse />`,
	DeviceSystemReboot:             `<tds:SystemRebootResponse><tds:Message>OK</tds:Message></tds:SystemRebootResponse>`,

	DeviceGetNetworkInterfaces: `<tds:GetNetworkInterfacesResponse />`,
	DeviceGetNetworkProtocols:  `<tds:GetNetworkProtocolsResponse />`,
	DeviceGetScopes: `<tds:GetScopesResponse>
	<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/name/go2rtc</tt:ScopeItem></tds:Scopes>
	<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/location/github</tt:ScopeItem></tds:Scopes>
	<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/Profile/Streaming</tt:ScopeItem></tds:Scopes>
	<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/type/Network_Video_Transmitter</tt:ScopeItem></tds:Scopes>
</tds:GetScopesResponse>`,

	MediaGetAudioEncoderConfigurations: `<trt:GetAudioEncoderConfigurationsResponse />`,
	MediaGetAudioSources:               `<trt:GetAudioSourcesResponse />`,
	MediaGetAudioSourceConfigurations:  `<trt:GetAudioSourceConfigurationsResponse />`,

	MediaGetVideoEncoderConfigurationOptions: `<trt:GetVideoEncoderConfigurationOptionsResponse>
   <trt:Options>
       <tt:QualityRange><tt:Min>1</tt:Min><tt:Max>6</tt:Max></tt:QualityRange>
	   <tt:H264>
		   <tt:ResolutionsAvailable><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:ResolutionsAvailable>
		   <tt:GovLengthRange><tt:Min>0</tt:Min><tt:Max>100</tt:Max></tt:GovLengthRange>
		   <tt:FrameRateRange><tt:Min>1</tt:Min><tt:Max>30</tt:Max></tt:FrameRateRange>
		   <tt:EncodingIntervalRange><tt:Min>1</tt:Min><tt:Max>100</tt:Max></tt:EncodingIntervalRange>
           <tt:H264ProfilesSupported>Main</tt:H264ProfilesSupported>
	   </tt:H264>
   </trt:Options>
</trt:GetVideoEncoderConfigurationOptionsResponse>`,

	"GetVideoSourceConfigurationOptions": `<trt:GetVideoSourceConfigurationOptionsResponse>
	<trt:Options>
		<tt:BoundsRange>
			<tt:XRange><tt:Min>0</tt:Min><tt:Max>0</tt:Max></tt:XRange>
			<tt:YRange><tt:Min>0</tt:Min><tt:Max>0</tt:Max></tt:YRange>
			<tt:WidthRange><tt:Min>640</tt:Min><tt:Max>3840</tt:Max></tt:WidthRange>
			<tt:HeightRange><tt:Min>360</tt:Min><tt:Max>2160</tt:Max></tt:HeightRange>
		</tt:BoundsRange>
		<tt:VideoSourceTokensAvailable>vsrc_main_stream</tt:VideoSourceTokensAvailable>
	</trt:Options>
</trt:GetVideoSourceConfigurationOptionsResponse>`,

	"GetCompatibleVideoSourceConfigurations": `<trt:GetCompatibleVideoSourceConfigurationsResponse>
	<trt:Configurations token="main_stream">
		<tt:Name>VSC_MainStream</tt:Name>
		<tt:UseCount>1</tt:UseCount>
		<tt:SourceToken>vsrc_main_stream</tt:SourceToken>
		<tt:Bounds x="0" y="0" width="1920" height="1080"></tt:Bounds>
	</trt:Configurations>
</trt:GetCompatibleVideoSourceConfigurationsResponse>`,

	"GetCompatibleVideoEncoderConfigurations": `<trt:GetCompatibleVideoEncoderConfigurationsResponse>
	<trt:Configurations token="main_stream">
		<tt:Name>VEC_MainStream</tt:Name>
		<tt:UseCount>1</tt:UseCount>
		<tt:Encoding>H264</tt:Encoding>
		<tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution>
		<tt:Quality>4</tt:Quality>
		<tt:RateControl><tt:FrameRateLimit>30</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>
		<tt:H264><tt:GovLength>30</tt:GovLength><tt:H264Profile>Main</tt:H264Profile></tt:H264>
		<tt:Multicast><tt:Address><tt:Type>IPv4</tt:Type><tt:IPv4Address>0.0.0.0</tt:IPv4Address></tt:Address><tt:Port>0</tt:Port><tt:TTL>1</tt:TTL><tt:AutoStart>false</tt:AutoStart></tt:Multicast>
		<tt:SessionTimeout>PT60S</tt:SessionTimeout>
	</trt:Configurations>
</trt:GetCompatibleVideoEncoderConfigurationsResponse>`,

	"GetGuaranteedNumberOfVideoEncoderInstances": `<trt:GetGuaranteedNumberOfVideoEncoderInstancesResponse><trt:TotalNumber>2</trt:TotalNumber><trt:H264>2</trt:H264></trt:GetGuaranteedNumberOfVideoEncoderInstancesResponse>`,
	"GetAudioOutputConfigurations":              `<trt:GetAudioOutputConfigurationsResponse />`,
	"GetAudioOutputs":                           `<trt:GetAudioOutputsResponse />`,
	"GetMetadataConfigurations":                 `<trt:GetMetadataConfigurationsResponse />`,
	"GetMetadataConfigurationOptions":           `<trt:GetMetadataConfigurationOptionsResponse />`,
	"GetEventProperties":                        `<tev:GetEventPropertiesResponse xmlns:tev="http://www.onvif.org/ver10/events/wsdl" />`,
	"GetUsers":                                  `<tds:GetUsersResponse />`,
	"GetWsdlUrl":                                `<tds:GetWsdlUrlResponse><tds:WsdlUrl>http://www.onvif.org/ver10/device/wsdl/devicemgmt.wsdl</tds:WsdlUrl></tds:GetWsdlUrlResponse>`,
	"GetEndpointReference":                      `<tds:GetEndpointReferenceResponse><tds:GUID>00000000-0000-0000-0000-000000000000</tds:GUID></tds:GetEndpointReferenceResponse>`,
}
