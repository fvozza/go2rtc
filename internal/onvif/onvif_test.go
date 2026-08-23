package onvif

import (
	"testing"

	"github.com/AlexxIT/go2rtc/pkg/yaml"
	"github.com/stretchr/testify/require"
)

func TestConfigUnmarshalStreamsList(t *testing.T) {
	src := `
onvif:
  dev: eth0
  streams:
    - cam1
    - cam2
`
	var cfg Config
	err := yaml.Unmarshal([]byte(src), &cfg)
	require.NoError(t, err)
	require.Equal(t, "eth0", cfg.Mod.Dev)
	require.Equal(t, []string{"cam1", "cam2"}, cfg.Mod.Streams)
	require.False(t, cfg.Mod.StreamsIsMap)
}

func TestConfigUnmarshalStreamsMapWithMAC(t *testing.T) {
	src := `
onvif:
  dev: eth0
  streams:
    cam1:
      mac: 1A:11:B0:12:34:56
      ipv4: 192.168.1.50/24
    cam2:
      mac: 1A:11:B0:78:90:AB
`
	var cfg Config
	err := yaml.Unmarshal([]byte(src), &cfg)
	require.NoError(t, err)
	require.Equal(t, "eth0", cfg.Mod.Dev)
	require.True(t, cfg.Mod.StreamsIsMap)
	require.Contains(t, cfg.Mod.Streams, "cam1")
	require.Contains(t, cfg.Mod.Streams, "cam2")

	require.NotNil(t, cfg.Mod.Devices["cam1"])
	require.Equal(t, "1A:11:B0:12:34:56", cfg.Mod.Devices["cam1"].MAC)
	require.Equal(t, "192.168.1.50/24", cfg.Mod.Devices["cam1"].IPv4)

	require.NotNil(t, cfg.Mod.Devices["cam2"])
	require.Equal(t, "1A:11:B0:78:90:AB", cfg.Mod.Devices["cam2"].MAC)
}

func TestConfigUnmarshalStreamsShorthandMAC(t *testing.T) {
	src := `
onvif:
  dev: eth0
  streams:
    cam1: 1A:11:B0:12:34:56
    cam2: 1A:11:B0:78:90:AB
`
	var cfg Config
	err := yaml.Unmarshal([]byte(src), &cfg)
	require.NoError(t, err)
	require.True(t, cfg.Mod.StreamsIsMap)
	require.Equal(t, "1A:11:B0:12:34:56", cfg.Mod.Devices["cam1"].MAC)
	require.Equal(t, "1A:11:B0:78:90:AB", cfg.Mod.Devices["cam2"].MAC)
}

func TestConfigUnmarshalStreamsListOfObjects(t *testing.T) {
	src := `
onvif:
  dev: eth0
  streams:
    - cam1
    - name: cam2
      mac: 1A:11:B0:78:90:AB
`
	var cfg Config
	err := yaml.Unmarshal([]byte(src), &cfg)
	require.NoError(t, err)
	require.Equal(t, []string{"cam1", "cam2"}, cfg.Mod.Streams)
	require.Equal(t, "1A:11:B0:78:90:AB", cfg.Mod.Devices["cam2"].MAC)
}

func TestConfigUnmarshalDevices(t *testing.T) {
	src := `
onvif:
  dev: eth0
  devices:
    cam1:
      mac: 1A:11:B0:12:34:56
`
	var cfg Config
	err := yaml.Unmarshal([]byte(src), &cfg)
	require.NoError(t, err)
	require.Equal(t, "1A:11:B0:12:34:56", cfg.Mod.Devices["cam1"].MAC)
}

func TestPreservedMACsLogic(t *testing.T) {
	src := `
onvif:
  dev: eth0
  streams:
    cam1:
      mac: 1A:11:B0:12:34:56
    cam2:
      mac: 1A-11-B0-78-90-AB
    cam3:
`
	var cfg Config
	err := yaml.Unmarshal([]byte(src), &cfg)
	require.NoError(t, err)

	allDevs := make(map[string]*DeviceConfig)
	for _, streamName := range cfg.Mod.Streams {
		if dev, exists := cfg.Mod.Devices[streamName]; exists {
			allDevs[streamName] = dev
		} else {
			allDevs[streamName] = &DeviceConfig{Name: streamName, Dev: cfg.Mod.Dev}
		}
	}

	require.Equal(t, 3, len(allDevs))
	require.Equal(t, "1A:11:B0:12:34:56", allDevs["cam1"].MAC)
	require.Equal(t, "1A-11-B0-78-90-AB", allDevs["cam2"].MAC)
	require.Empty(t, allDevs["cam3"].MAC)
}
