package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server         ServerConfig `yaml:"server"`
	Authentication AuthConfig   `yaml:"authentication"`
	Defaults       Defaults     `yaml:"defaults"`
	Devices        []Device     `yaml:"devices"`
}
type ServerConfig struct {
	Address           string `yaml:"address"`
	ScansDirectory    string `yaml:"scans_directory"`
	StaticDirectory   string `yaml:"static_directory"`
	RequestLimit      string `yaml:"request_limit"`
	RequestLimitBytes int64  `yaml:"-"`
}
type AuthConfig struct {
	Enabled             bool          `yaml:"enabled"`
	Username            string        `yaml:"username"`
	Password            string        `yaml:"password"`
	SessionLifetime     time.Duration `yaml:"-"`
	SessionLifetimeText string        `yaml:"session_lifetime"`
	SecureCookie        bool          `yaml:"secure_cookie"`
}
type Defaults struct {
	Device string       `yaml:"device"`
	Scan   ScanDefaults `yaml:"scan"`
}
type ScanDefaults struct {
	DPI    int    `yaml:"dpi"`
	Mode   string `yaml:"mode"`
	Format string `yaml:"format"`
}
type Device struct {
	ID    string       `yaml:"id" json:"id"`
	Name  string       `yaml:"name" json:"name"`
	Scan  *ScanConfig  `yaml:"scan,omitempty" json:"scan,omitempty"`
	Print *PrintConfig `yaml:"print,omitempty" json:"print,omitempty"`
}
type ScanConfig struct {
	Driver   string     `yaml:"driver" json:"driver"`
	Endpoint string     `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Device   string     `yaml:"device,omitempty" json:"device,omitempty"`
	TLS      TLSConfig  `yaml:"tls,omitempty" json:"-"`
	Auth     DeviceAuth `yaml:"auth,omitempty" json:"-"`
}
type PrintConfig struct {
	Driver string `yaml:"driver" json:"driver"`
	Queue  string `yaml:"queue" json:"queue"`
}
type TLSConfig struct {
	Verify            bool   `yaml:"verify"`
	CAFile            string `yaml:"ca_file,omitempty"`
	ServerName        string `yaml:"server_name,omitempty"`
	SHA256Fingerprint string `yaml:"sha256_fingerprint,omitempty"`
}
type DeviceAuth struct {
	Type     string `yaml:"type"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}

var appConfig Config

func loadConfig(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err = yaml.Unmarshal(b, &appConfig); err != nil {
		return fmt.Errorf("parse configuration: %w", err)
	}
	if appConfig.Server.Address == "" {
		appConfig.Server.Address = ":8085"
	}
	if appConfig.Server.ScansDirectory == "" {
		appConfig.Server.ScansDirectory = "/app/scans"
	}
	if appConfig.Server.StaticDirectory == "" {
		appConfig.Server.StaticDirectory = "/app/static"
	}
	if appConfig.Server.RequestLimit == "" {
		appConfig.Server.RequestLimit = "500MiB"
	}
	appConfig.Server.RequestLimitBytes, err = parseByteSize(appConfig.Server.RequestLimit)
	if err != nil {
		return fmt.Errorf("server.request_limit: %w", err)
	}
	if appConfig.Defaults.Scan.DPI == 0 {
		appConfig.Defaults.Scan.DPI = 300
	}
	if appConfig.Defaults.Scan.Mode == "" {
		appConfig.Defaults.Scan.Mode = "Color"
	}
	if appConfig.Defaults.Scan.Format == "" {
		appConfig.Defaults.Scan.Format = "pdf"
	}
	if appConfig.Authentication.SessionLifetimeText == "" {
		appConfig.Authentication.SessionLifetime = 7 * 24 * time.Hour
	} else if appConfig.Authentication.SessionLifetime, err = time.ParseDuration(appConfig.Authentication.SessionLifetimeText); err != nil {
		return fmt.Errorf("authentication.session_lifetime: %w", err)
	}
	seen := map[string]bool{}
	for i := range appConfig.Devices {
		d := &appConfig.Devices[i]
		if d.ID == "" || d.Name == "" || seen[d.ID] {
			return fmt.Errorf("devices[%d] must have a unique id and name", i)
		}
		seen[d.ID] = true
		if d.Scan != nil && d.Scan.Driver != "sane" && d.Scan.Driver != "escl" {
			return fmt.Errorf("device %q: unsupported scan driver %q", d.ID, d.Scan.Driver)
		}
		if d.Print != nil && d.Print.Driver != "cups" {
			return fmt.Errorf("device %q: unsupported print driver %q", d.ID, d.Print.Driver)
		}
	}
	if appConfig.Defaults.Device == "" && len(appConfig.Devices) > 0 {
		appConfig.Defaults.Device = appConfig.Devices[0].ID
	}
	return nil
}

func parseByteSize(s string) (int64, error) {
	v := strings.TrimSpace(strings.ToUpper(s))
	units := []struct {
		name       string
		multiplier int64
	}{{"GIB", 1 << 30}, {"MIB", 1 << 20}, {"KIB", 1 << 10}, {"GB", 1_000_000_000}, {"MB", 1_000_000}, {"KB", 1_000}, {"B", 1}}
	for _, unit := range units {
		if strings.HasSuffix(v, unit.name) {
			n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(v, unit.name)), 10, 64)
			if err != nil || n <= 0 {
				return 0, fmt.Errorf("invalid size %q", s)
			}
			return n * unit.multiplier, nil
		}
	}
	return 0, fmt.Errorf("invalid size %q", s)
}

func configuredDevice(id string) (*Device, error) {
	if id == "" {
		id = appConfig.Defaults.Device
	}
	for i := range appConfig.Devices {
		if appConfig.Devices[i].ID == id {
			return &appConfig.Devices[i], nil
		}
	}
	return nil, fmt.Errorf("unknown device %q", id)
}
