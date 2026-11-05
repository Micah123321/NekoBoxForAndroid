package mieru

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/enfein/mieru/v3/apis/trafficpattern"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/sagernet/sing-box/option"
)

type Options struct {
	option.DialerOptions
	option.ServerOptions
	ServerPorts    []string `json:"server_ports,omitempty"`
	Username       string   `json:"username,omitempty"`
	Password       string   `json:"password,omitempty"`
	Transport      string   `json:"transport,omitempty"`
	Multiplexing   string   `json:"multiplexing,omitempty"`
	TrafficPattern string   `json:"traffic_pattern,omitempty"`
	HandshakeMode  string   `json:"handshake_mode,omitempty"`
	MTU            int      `json:"mtu,omitempty"`
}

type serverPort struct{ begin, end int }

type normalizedOptions struct {
	Options
	transport     appctlpb.TransportProtocol
	ports         []serverPort
	multiplexing  *appctlpb.MultiplexingLevel
	handshakeMode *appctlpb.HandshakeMode
	mtu           int
}

func normalizeOptions(options Options) (normalizedOptions, error) {
	if options.Server == "" {
		return normalizedOptions{}, fmt.Errorf("server is empty")
	}
	if options.Username == "" {
		return normalizedOptions{}, fmt.Errorf("username is empty")
	}
	if options.Password == "" {
		return normalizedOptions{}, fmt.Errorf("password is empty")
	}
	if options.Transport == "" {
		options.Transport = "TCP"
	}
	if options.Transport != "TCP" && options.Transport != "UDP" {
		return normalizedOptions{}, fmt.Errorf("transport must be TCP or UDP")
	}
	transport := appctlpb.TransportProtocol_TCP
	if options.Transport == "UDP" {
		transport = appctlpb.TransportProtocol_UDP
	}
	ports := make([]serverPort, 0, len(options.ServerPorts)+1)
	if options.ServerPort != 0 {
		ports = append(ports, serverPort{begin: int(options.ServerPort), end: int(options.ServerPort)})
	}
	for _, value := range options.ServerPorts {
		parts := strings.Split(strings.TrimSpace(value), "-")
		if len(parts) < 1 || len(parts) > 2 || parts[0] == "" {
			return normalizedOptions{}, fmt.Errorf("invalid server port range %q", value)
		}
		begin, err := strconv.Atoi(parts[0])
		if err != nil {
			return normalizedOptions{}, fmt.Errorf("invalid server port range %q", value)
		}
		end := begin
		if len(parts) == 2 {
			end, err = strconv.Atoi(parts[1])
			if err != nil {
				return normalizedOptions{}, fmt.Errorf("invalid server port range %q", value)
			}
		}
		if begin < 1 || end < begin || end > 65535 {
			return normalizedOptions{}, fmt.Errorf("invalid server port range %q", value)
		}
		ports = append(ports, serverPort{begin: begin, end: end})
	}
	if len(ports) == 0 {
		return normalizedOptions{}, fmt.Errorf("either server_port or server_ports must be set")
	}
	if options.MTU == 0 {
		options.MTU = 1400
	}
	if options.MTU < 1280 || options.MTU > 1400 {
		return normalizedOptions{}, fmt.Errorf("mtu must be between 1280 and 1400")
	}
	var multiplexing *appctlpb.MultiplexingLevel
	if options.Multiplexing != "" {
		name := strings.ToUpper(options.Multiplexing)
		if !strings.HasPrefix(name, "MULTIPLEXING_") {
			name = "MULTIPLEXING_" + name
		}
		level, ok := appctlpb.MultiplexingLevel_value[name]
		if !ok || name == "MULTIPLEXING_DEFAULT" {
			return normalizedOptions{}, fmt.Errorf("invalid multiplexing level: %s", options.Multiplexing)
		}
		multiplexing = appctlpb.MultiplexingLevel(level).Enum()
		options.Multiplexing = name
	}
	var handshake *appctlpb.HandshakeMode
	if options.HandshakeMode != "" {
		name := strings.ToUpper(options.HandshakeMode)
		if !strings.HasPrefix(name, "HANDSHAKE_") {
			name = "HANDSHAKE_" + name
		}
		mode, ok := appctlpb.HandshakeMode_value[name]
		if !ok {
			return normalizedOptions{}, fmt.Errorf("invalid handshake mode: %s", options.HandshakeMode)
		}
		handshake = appctlpb.HandshakeMode(mode).Enum()
		options.HandshakeMode = name
	}
	if options.TrafficPattern != "" {
		pattern, err := trafficpattern.Decode(options.TrafficPattern)
		if err != nil {
			return normalizedOptions{}, fmt.Errorf("failed to decode traffic pattern: %w", err)
		}
		if err := trafficpattern.Validate(pattern); err != nil {
			return normalizedOptions{}, fmt.Errorf("invalid traffic pattern: %w", err)
		}
	}
	return normalizedOptions{Options: options, transport: transport, ports: ports, multiplexing: multiplexing, handshakeMode: handshake, mtu: options.MTU}, nil
}
