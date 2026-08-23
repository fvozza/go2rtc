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

This fork is insipired by [rtsp2onvif](https://github.com/p10tyr/rtsp-to-onvif) and adds macvlan support for go2rtc, allowing each stream to be published as a virtual onvif camera with its own mac address and IP address obtained via dhcp.

Useful to add go2rtc streams to NVRs such as Ubiquiti Unifi Protect or Synology Surveillance Station.
