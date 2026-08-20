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

---

## ONVIF Server

Go2rtc can act as an ONVIF Profile S server to expose streams to NVRs (like **UniFi Protect 5+**, Synology Surveillance Station, Scrypted, Home Assistant, etc.).

It includes a built-in **WS-Discovery responder** (UDP 3702 multicast) so NVRs can automatically discover virtual cameras on the local network.

### 1. Selective Stream Publishing (Recommended for UniFi Protect)

You can choose exactly which streams to publish as virtual ONVIF cameras:

```yaml
onvif:
  enabled: true      # Enable/disable virtual ONVIF camera generation (default: true)
  dev: eth0          # Parent network interface for automatic MacVLAN + DHCP
  discovery: true    # Enable WS-Discovery responder on 239.255.255.250:3702 (default: true)
  http_port: 80      # HTTP port on virtual IP (default: 80)
  rtsp_port: 8554    # RTSP port on virtual IP (default: 8554)

  # Choose which streams to publish as virtual ONVIF cameras:
  streams:
    - cam-ber-0
    - cam-ta-1

streams:
  cam-ber-0: rtsp://192.168.1.50:554/ch0
  cam-ber-1: rtsp://192.168.1.51:554/ch0  # not published to ONVIF
  cam-ta-1:  rtsp://192.168.1.52:554/ch0
```

*Every selected stream receives:*
- Isolated MacVLAN interface (`go2rtc_onvif_0`, `go2rtc_onvif_1`, etc.).
- Unique DHCP IP address (e.g. `192.168.1.181`, `192.168.1.182`).
- Isolated ONVIF Profile S SOAP service exposing **only that single stream** as `MainStream`.
- Snapshot endpoint (`http://<virtual_ip>:80/snapshot.png` redirecting to frame JPEG).
- Automatic WS-Discovery advertisement.

> **Note**: Virtual ONVIF cameras are only published for streams explicitly listed in `onvif.streams` or `onvif.devices`. If neither is specified, no virtual cameras will be created.

### 2. Optional Per-Stream Customization

You can optionally override settings (e.g. static IP, MAC, custom name) for individual streams:

```yaml
onvif:
  dev: eth0
  devices:
    cam-ber-0:
      name: "Front Yard Camera"
      # mac: 1A:11:B0:12:34:56                   # Auto-generated & persisted if omitted
      # ipv4: 192.168.1.181/24                   # Static IP if not using DHCP
      # http_port: 8080
      # rtsp_port: 554

streams:
  cam-ber-0: rtsp://192.168.1.50:554/ch0
```
