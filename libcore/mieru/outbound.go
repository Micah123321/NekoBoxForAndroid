package mieru

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/log"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	mieruclient "github.com/enfein/mieru/v3/apis/client"
	mierucommon "github.com/enfein/mieru/v3/apis/common"
	mierumodel "github.com/enfein/mieru/v3/apis/model"
	mierutp "github.com/enfein/mieru/v3/apis/trafficpattern"
	mierupb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

type Outbound struct {
	outbound.Adapter
	dialer N.Dialer
	logger log.ContextLogger
	client mieruclient.Client
}

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register(registry, "mieru", NewOutbound)
}

func NewOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options Options) (adapter.Outbound, error) {
	outboundDialer, err := dialer.New(ctx, options.DialerOptions, M.IsDomainName(options.Server))
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	var resolver mierucommon.DNSResolver
	if dnsRouter, ok := router.(adapter.DNSRouter); ok {
		resolver = singBoxDNSResolver{router: dnsRouter}
	}
	config, err := buildMieruClientConfig(normalized, mieruDialer{dialer: outboundDialer}, resolver)
	if err != nil {
		return nil, fmt.Errorf("failed to build mieru client config: %w", err)
	}
	client := mieruclient.NewClient()
	if err := client.Store(config); err != nil {
		return nil, fmt.Errorf("failed to store mieru client config: %w", err)
	}
	if err := client.Start(); err != nil {
		return nil, fmt.Errorf("failed to start mieru client: %w", err)
	}
	logger.InfoContext(ctx, "mieru client is started")
	return &Outbound{
		Adapter: outbound.NewAdapterWithDialerOptions("mieru", tag, []string{N.NetworkTCP, N.NetworkUDP}, options.DialerOptions),
		dialer:  outboundDialer,
		logger:  logger,
		client:  client,
	}, nil
}

func (o *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = o.Tag()
	metadata.Destination = destination
	switch N.NetworkName(network) {
	case N.NetworkTCP:
		o.logger.InfoContext(ctx, "outbound connection to ", destination)
		d, err := socksAddrToNetAddrSpec(destination, "tcp")
		if err != nil {
			return nil, E.Cause(err, "failed to convert destination address")
		}
		return o.client.DialContext(ctx, d)
	case N.NetworkUDP:
		o.logger.InfoContext(ctx, "outbound UoT packet connection to ", destination)
		d, err := socksAddrToNetAddrSpec(destination, "udp")
		if err != nil {
			return nil, E.Cause(err, "failed to convert destination address")
		}
		streamConn, err := o.client.DialContext(ctx, d)
		if err != nil {
			return nil, err
		}
		return &streamer{
			PacketConn: mierucommon.NewUDPAssociateWrapper(mierucommon.NewPacketOverStreamTunnel(streamConn)),
			Remote:     destination,
		}, nil
	default:
		return nil, os.ErrInvalid
	}
}

func (o *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = o.Tag()
	metadata.Destination = destination
	o.logger.InfoContext(ctx, "outbound UoT packet connection to ", destination)
	d, err := socksAddrToNetAddrSpec(destination, "udp")
	if err != nil {
		return nil, E.Cause(err, "failed to convert destination address")
	}
	streamConn, err := o.client.DialContext(ctx, d)
	if err != nil {
		return nil, err
	}
	return mierucommon.NewUDPAssociateWrapper(mierucommon.NewPacketOverStreamTunnel(streamConn)), nil
}

func (o *Outbound) Close() error {
	return o.client.Stop()
}

// mieruDialer is an adapter to mieru dialer interface.
type mieruDialer struct {
	dialer N.Dialer
}

func (md mieruDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	addr := M.ParseSocksaddr(address)
	return md.dialer.DialContext(ctx, network, addr)
}

func (md mieruDialer) ListenPacket(ctx context.Context, network, laddr, raddr string) (net.PacketConn, error) {
	addr := M.ParseSocksaddr(raddr)
	return md.dialer.ListenPacket(ctx, addr)
}

var (
	_ mierucommon.Dialer       = (*mieruDialer)(nil)
	_ mierucommon.PacketDialer = (*mieruDialer)(nil)
)

// streamer converts a net.PacketConn to a net.Conn.
type streamer struct {
	net.PacketConn
	Remote net.Addr
}

func newStreamer(conn net.Conn, remote net.Addr) *streamer {
	return &streamer{PacketConn: mierucommon.NewUDPAssociateWrapper(mierucommon.NewPacketOverStreamTunnel(conn)), Remote: remote}
}

var _ net.Conn = (*streamer)(nil)

func (s *streamer) Read(b []byte) (n int, err error) {
	n, _, err = s.PacketConn.ReadFrom(b)
	return
}

func (s *streamer) Write(b []byte) (n int, err error) {
	return s.WriteTo(b, s.Remote)
}

func (s *streamer) RemoteAddr() net.Addr {
	return s.Remote
}

// socksAddrToNetAddrSpec converts a Socksaddr object to NetAddrSpec, and overrides the network.
func socksAddrToNetAddrSpec(sa M.Socksaddr, network string) (mierumodel.NetAddrSpec, error) {
	var nas mierumodel.NetAddrSpec
	if err := nas.From(sa); err != nil {
		return nas, err
	}
	nas.Net = network
	return nas, nil
}

func buildMieruClientConfig(options normalizedOptions, dialer mieruDialer, resolvers ...mierucommon.DNSResolver) (*mieruclient.ClientConfig, error) {
	server := &mierupb.ServerEndpoint{}
	for _, port := range options.ports {
		binding := &mierupb.PortBinding{Protocol: options.transport.Enum()}
		if port.begin == port.end {
			binding.Port = proto.Int32(int32(port.begin))
		} else {
			binding.PortRange = proto.String(fmt.Sprintf("%d-%d", port.begin, port.end))
		}
		server.PortBindings = append(server.PortBindings, binding)
	}
	if M.IsDomainName(options.Server) {
		server.DomainName = proto.String(options.Server)
	} else {
		server.IpAddress = proto.String(options.Server)
	}
	config := &mieruclient.ClientConfig{
		Profile: &mierupb.ClientProfile{
			ProfileName: proto.String("sing-box"),
			User:        &mierupb.User{Name: proto.String(options.Username), Password: proto.String(options.Password)},
			Servers:     []*mierupb.ServerEndpoint{server},
			Mtu:         proto.Int32(int32(options.mtu)),
			Multiplexing: func() *mierupb.MultiplexingConfig {
				if options.multiplexing == nil {
					return nil
				}
				return &mierupb.MultiplexingConfig{Level: options.multiplexing}
			}(),
			HandshakeMode: options.handshakeMode,
		},
		Dialer:       dialer,
		PacketDialer: dialer,
		DNSConfig:    &mierucommon.ClientDNSConfig{BypassDialerDNS: true},
	}
	if len(resolvers) > 0 && resolvers[0] != nil {
		config.Resolver = resolvers[0]
	}
	if options.TrafficPattern != "" {
		trafficPattern, err := mierutp.Decode(options.TrafficPattern)
		if err != nil {
			return nil, err
		}
		config.Profile.TrafficPattern = trafficPattern
	}
	return config, nil
}

type singBoxDNSResolver struct{ router adapter.DNSRouter }

func (r singBoxDNSResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	if network != "ip4" && network != "ip6" {
		return nil, fmt.Errorf("unsupported DNS network %q", network)
	}
	addresses, err := r.router.Lookup(ctx, host, adapter.DNSQueryOptions{})
	if err != nil {
		return nil, err
	}
	result := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if network == "ip4" && !address.Is4() || network == "ip6" && address.Is4() {
			continue
		}
		result = append(result, net.IP(address.AsSlice()))
	}
	return result, nil
}

func beginAndEndPortFromPortRange(portRange string) (int, int, error) {
	var begin, end int
	_, err := fmt.Sscanf(portRange, "%d-%d", &begin, &end)
	return begin, end, err
}
