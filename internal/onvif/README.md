# ONVIF

## ONVIF Client

[`new in v1.5.0`](https://github.com/AlexxIT/go2rtc/releases/tag/v1.5.0)

The source is not very useful if you already know RTSP and snapshot links for your camera. But it can be useful if you don't.

**WebUI > Add** webpage supports ONVIF autodiscovery. Your server must be on the same subnet as the camera. If you use Docker, you must use "network host".

```yaml
streams:
  dahua1: onvif://admin:password@192.168.1.123
  reolink1: onvif://admin:password@192.168.1.123:8000
  tapo1: onvif://admin:password@192.168.1.123:2020
```

## ONVIF Server

Go2rtc can act as an ONVIF Profile S server to expose streams to NVRs (like **UniFi Protect 5+**, Synology Surveillance Station, Scrypted, Home Assistant, etc.).

It includes a built-in **WS-Discovery responder** (UDP 3702 multicast) so NVRs can automatically discover virtual cameras on the local network.

### 1. Default Shared Server Mode

By default, all streams configured in go2rtc are discoverable and available via the main API port:

```yaml
onvif:
  server:
    listen: ":8080" # optional custom port for ONVIF SOAP services
    discovery: true # enable/disable WS-Discovery responder (default: true)
```

### 2. Virtual Camera Emulation (UniFi Protect / Dedicated IP Mode)

UniFi Protect and some NVRs require each camera to have its own unique MAC and IP address on the local network:

```yaml
onvif:
  devices:
    front_camera:
      name: FrontCam
      dev: eth0                                  # Network interface for MacVLAN (Linux only)
      # mac: 1A:11:B0:12:34:56                   # Auto-generated if omitted
      # uuid: 44302cbf-0d18-4feb-79b3-33b575263da3 # Auto-generated if omitted
      listen: "192.168.1.187:80"                 # Virtual IP to bind (or DHCP via MacVLAN)
      profiles:
        main:
          stream: front_hq                       # go2rtc stream name
          width: 2560
          height: 1440
          framerate: 25
          bitrate: 4096
        sub:
          stream: front_lq
          width: 640
          height: 360
          framerate: 15
          bitrate: 1024

streams:
  front_hq: rtsp://192.168.1.50:554/live0
  front_lq: rtsp://192.168.1.50:554/live1
```

### 3. `rtsp-to-onvif` Compatibility Syntax

You can also use the configuration format from `rtsp-to-onvif`:

```yaml
onvif:
  - name: BulletCam
    dev: eth0
    highQuality:
      rtsp: /Streaming/Channels/101/
      width: 2048
      height: 1536
      framerate: 15
      bitrate: 3072
    ports:
      server: 8081
      rtsp: 8554
      snapshot: 8080
```

## Tested clients

Go2rtc works as ONVIF server:

- UniFi Protect 5+ (Adoption & Recording)
- Synology Surveillance Station
- Scrypted
- Home Assistant ONVIF integration (linux)
- Happytime onvif client (windows)
- Onvier (android)
- ONVIF Device Manager (windows)

PS. Supports only TCP transport for RTSP protocol. UDP and HTTP transports - unsupported yet.

## Tested cameras

Go2rtc works as ONVIF client:

- Dahua IPC-K42
- OpenIPC
- Reolink RLC-520A
- TP-Link Tapo TC60

