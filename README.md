<h1 align="center">
  <a href="https://github.com/AlexxIT/go2rtc">
    <img src="./website/images/logo.gif" alt="go2rtc - GitHub">
  </a>
</h1>
<p align="center">
  <a href="https://github.com/AlexxIT/go2rtc/stargazers" target="_blank">
    <img style="display: inline" src="https://img.shields.io/github/stars/AlexxIT/go2rtc?style=flat-square&logo=github" alt="go2rtc - GitHub Stars">
  </a>
  <a href="https://hub.docker.com/r/alexxit/go2rtc" target="_blank">
    <img style="display: inline" src="https://img.shields.io/docker/pulls/alexxit/go2rtc?style=flat-square&logo=docker&logoColor=white&label=pulls" alt="go2rtc - Docker Pulls">
  </a>
  <a href="https://github.com/AlexxIT/go2rtc/releases" target="_blank">
    <img style="display: inline" src="https://img.shields.io/github/downloads/AlexxIT/go2rtc/total?color=blue&style=flat-square&logo=github" alt="go2rtc - GitHub Downloads">
  </a>
</p>
<p align="center">
  <a href="https://trendshift.io/repositories/4628" target="_blank">
    <img src="https://trendshift.io/api/badge/repositories/4628" alt="go2rtc - Trendshift"/>
  </a>
</p>

Ultimate camera streaming application with support for dozens formats and protocols.

---

### 🔀 Macvlan branch: [frvozza/go2rtc:macvlan](https://github.com/fvozza/go2rtc/tree/macvlan)

This fork is insipired by [rtsp2onvif](https://github.com/p10tyr/rtsp-to-onvif) and adds macvlan support for [go2rtc](https://github.com/AlexxIT/go2rtc), allowing each stream to be published as a virtual onvif camera with its own mac address and IP address obtained via dhcp.

Useful for adding go2rtc streams to NVRs such as Ubiquiti Unifi Protect or Synology Surveillance Station.

## How to use

### Docker Compose


```yaml
services:
  go2rtc:
    image: ghcr.io/fvozza/go2rtc:macvlan
    container_name: go2rtc-macvlan  # Optional
    network_mode: host       # important for WebRTC, HomeKit, UDP cameras
    privileged: true         # only for FFmpeg hardware transcoding
    devices:
      - /dev/bus/usb:/dev/bus/usb
      - /dev/dri:/dev/dri
    environment:
      - TZ=Europe/Berlin  # Timezone for the logs
    volumes:
      - /path/to/config:/config   # folder for go2rtc.yaml file (edit from WebUI)
    restart: unless-stopped  # autorestart on fail or config change from WebUI
```

### Config file

```yaml
rtsp:
  listen: ":555"    # RTSP Server TCP port, default - 8554

onvif:
  enabled: true     # Enable/disable virtual ONVIF camera generation (default: true)
  dev: eth0          # Parent network interface for automatic MacVLAN + DHCP
  discovery: true    # Enable WS-Discovery responder on 239.255.255.250:3702 (default: true)
  http_port: 9000    # HTTP port on virtual IP (default: 80)
  rtsp_port: 8554    # RTSP port on virtual IP (default: 8554)

  # Explicitly select which streams to publish as virtual ONVIF cameras. 
  # The names of the streams must match the names defined in the rtsp section.
  # This section is optional. If onvif.streams is empty, all streams will be published as virtual ONVIF cameras.
  # MAC and UUID are optional. If not provided, they will be generated automatically and added to the config file.
  # The format for mac is: xx:xx:xx:xx:xx:xx
  # The format for uuid is: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
  streams:
    - cam-1
    - cam-2

streams:
  cam-1: rtsp://user:pass@ip:port/stream
  cam-2: rtsp://user:pass@ip:port/stream
```