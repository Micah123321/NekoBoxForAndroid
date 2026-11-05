package mieru

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	mieruclient "github.com/enfein/mieru/v3/apis/client"
	mierucommon "github.com/enfein/mieru/v3/apis/common"
	mierupb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
)

func validOptions() Options {
	return Options{
		ServerOptions: option.ServerOptions{Server: "127.0.0.1"},
		ServerPorts:   []string{"8443", "9000-9002"},
		Username:      "user",
		Password:      "password",
	}
}

func TestNormalizeOptions(t *testing.T) {
	got, err := normalizeOptions(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got.Transport != "TCP" || got.transport != mierupb.TransportProtocol_TCP {
		t.Fatalf("default transport = %q/%v, want TCP", got.Transport, got.transport)
	}
	if len(got.ports) != 2 || got.ports[0] != (serverPort{begin: 8443, end: 8443}) || got.ports[1] != (serverPort{begin: 9000, end: 9002}) {
		t.Fatalf("ports = %#v", got.ports)
	}
	for _, value := range []string{"OFF", "LOW", "MIDDLE", "HIGH", "MULTIPLEXING_HIGH"} {
		options := validOptions()
		options.Multiplexing = value
		normalized, err := normalizeOptions(options)
		if err != nil {
			t.Fatalf("multiplexing %q: %v", value, err)
		}
		if normalized.multiplexing == nil {
			t.Fatalf("multiplexing %q was not set", value)
		}
	}
}

func TestNormalizeOptionsAcceptsEmbeddedSinglePort(t *testing.T) {
	options := validOptions()
	options.ServerPorts = nil
	options.ServerPort = 443
	normalized, err := normalizeOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.ports) != 1 || normalized.ports[0] != (serverPort{begin: 443, end: 443}) {
		t.Fatalf("ports = %#v", normalized.ports)
	}
}

func TestNormalizeOptionsRejectsMalformedConfig(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Options)
	}{
		{"empty server", func(o *Options) { o.Server = "" }},
		{"empty ports", func(o *Options) { o.ServerPorts = nil }},
		{"zero port", func(o *Options) { o.ServerPorts = []string{"0"} }},
		{"large port", func(o *Options) { o.ServerPorts = []string{"65536"} }},
		{"reversed range", func(o *Options) { o.ServerPorts = []string{"10-9"} }},
		{"malformed range", func(o *Options) { o.ServerPorts = []string{"10-11-12"} }},
		{"lowercase transport", func(o *Options) { o.Transport = "tcp" }},
		{"empty username", func(o *Options) { o.Username = "" }},
		{"empty password", func(o *Options) { o.Password = "" }},
		{"default multiplexing enum", func(o *Options) { o.Multiplexing = "MULTIPLEXING_DEFAULT" }},
		{"invalid mtu", func(o *Options) { o.MTU = 1279 }},
		{"invalid traffic pattern", func(o *Options) { o.TrafficPattern = "%%%" }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			options := validOptions()
			testCase.edit(&options)
			if _, err := normalizeOptions(options); err == nil {
				t.Fatal("normalizeOptions() returned nil error")
			}
		})
	}
}

func TestBuildMieruClientConfig(t *testing.T) {
	options := validOptions()
	options.Server = "2001:db8::1"
	options.Transport = "UDP"
	options.Multiplexing = "HIGH"
	options.MTU = 1400
	normalized, err := normalizeOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	config, err := buildMieruClientConfig(normalized, mieruDialer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	profile := config.Profile
	if profile.GetServers()[0].GetIpAddress() != options.Server {
		t.Fatalf("server IP = %q", profile.GetServers()[0].GetIpAddress())
	}
	bindings := profile.GetServers()[0].GetPortBindings()
	if len(bindings) != 2 || bindings[0].GetPort() != 8443 || bindings[1].GetPortRange() != "9000-9002" {
		t.Fatalf("port bindings = %#v", bindings)
	}
	if bindings[0].GetProtocol() != mierupb.TransportProtocol_UDP || profile.GetMultiplexing().GetLevel() != mierupb.MultiplexingLevel_MULTIPLEXING_HIGH || profile.GetMtu() != 1400 {
		t.Fatalf("profile fields were not mapped: %v %v %d", bindings[0].GetProtocol(), profile.GetMultiplexing().GetLevel(), profile.GetMtu())
	}
	if config.DNSConfig == nil || !config.DNSConfig.BypassDialerDNS {
		t.Fatal("Mieru must pass domain resolution to the sing-box dialer")
	}
}

type recordingDialer struct {
	mu             sync.Mutex
	networks       []string
	destinations   []string
	packetNetworks []string
	packetTargets  []string
	peer           net.Conn
}

func (d *recordingDialer) DialContext(_ context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	client, peer := net.Pipe()
	d.mu.Lock()
	d.networks = append(d.networks, network)
	d.destinations = append(d.destinations, destination.String())
	d.peer = peer
	d.mu.Unlock()
	return client, nil
}

func (d *recordingDialer) ListenPacket(_ context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	d.mu.Lock()
	d.packetNetworks = append(d.packetNetworks, "udp")
	d.packetTargets = append(d.packetTargets, destination.String())
	d.mu.Unlock()
	return net.ListenPacket("udp", "127.0.0.1:0")
}

func TestMieruDialerDelegatesTCPAndUDPToSingBoxDialer(t *testing.T) {
	recorder := &recordingDialer{}
	md := mieruDialer{dialer: recorder}
	conn, err := md.DialContext(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	recorder.mu.Lock()
	if recorder.peer != nil {
		recorder.peer.Close()
	}
	recorder.mu.Unlock()
	packetConn, err := md.ListenPacket(context.Background(), "udp", "", "[::1]:53")
	if err != nil {
		t.Fatal(err)
	}
	packetConn.Close()
	if len(recorder.networks) != 1 || recorder.networks[0] != "tcp" || recorder.destinations[0] != "example.com:443" {
		t.Fatalf("TCP calls = %#v/%#v", recorder.networks, recorder.destinations)
	}
	if len(recorder.packetTargets) != 1 || recorder.packetTargets[0] != "[::1]:53" {
		t.Fatalf("UDP calls = %#v", recorder.packetTargets)
	}
}

func TestMieruUDPWrappersRoundTrip(t *testing.T) {
	left, right := net.Pipe()
	stream := newStreamer(left, &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 5353})
	peer := mierucommon.NewUDPAssociateWrapper(mierucommon.NewPacketOverStreamTunnel(right))
	defer stream.Close()
	defer peer.Close()

	payload := []byte("mieru udp payload")
	result := make(chan struct {
		n    int
		addr net.Addr
		err  error
		data string
	}, 1)
	go func() {
		buf := make([]byte, 128)
		n, addr, err := peer.ReadFrom(buf)
		result <- struct {
			n    int
			addr net.Addr
			err  error
			data string
		}{n: n, addr: addr, err: err, data: string(buf[:n])}
	}()
	if n, err := stream.Write(payload); err != nil || n != len(payload) {
		t.Fatalf("stream Write() = %d, %v", n, err)
	}
	select {
	case got := <-result:
		if got.err != nil || got.n != len(payload) || got.data != string(payload) {
			t.Fatalf("wrapped packet = %#v", got)
		}
		if got.addr.String() != "192.0.2.1:5353" {
			t.Fatalf("wrapped address = %v", got.addr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out reading wrapped UDP packet")
	}
}

type fakeDNSRouter struct {
	adapter.DNSRouter
	addresses []netip.Addr
	calls     int
}

func (r *fakeDNSRouter) Lookup(_ context.Context, _ string, _ adapter.DNSQueryOptions) ([]netip.Addr, error) {
	r.calls++
	return r.addresses, nil
}

func TestSingBoxDNSResolverUsesRouterAndFiltersFamily(t *testing.T) {
	router := &fakeDNSRouter{addresses: []netip.Addr{netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("2001:db8::10")}}
	resolver := singBoxDNSResolver{router: router}
	ips, err := resolver.LookupIP(context.Background(), "ip4", "mieru.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 1 || !ips[0].Equal(net.ParseIP("192.0.2.10")) || router.calls != 1 {
		t.Fatalf("resolver result = %v, calls = %d", ips, router.calls)
	}
	all, err := resolver.LookupIP(context.Background(), "ip", "mieru.example")
	if err != nil || len(all) != 2 {
		t.Fatalf("dual-stack lookup = %v, %v", all, err)
	}
	if _, err := resolver.LookupIP(context.Background(), "tcp", "mieru.example"); err == nil {
		t.Fatal("unknown network was accepted")
	}
}

func TestOutboundUsesDNSRouterFromContext(t *testing.T) {
	router := &fakeDNSRouter{addresses: []netip.Addr{netip.MustParseAddr("192.0.2.10")}}
	ctx := service.ContextWith[adapter.DNSRouter](context.Background(), router)
	raw, err := NewOutbound(ctx, nil, log.NewNOPFactory().Logger(), "test", validOptions())
	if err != nil {
		t.Fatal(err)
	}
	outbound := raw.(*Outbound)
	defer outbound.Close()
	config, err := outbound.client.Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.Resolver == nil {
		t.Fatal("missing DNS resolver from sing-box context")
	}
	ips, err := config.Resolver.LookupIP(ctx, "ip4", "example.com")
	if err != nil || len(ips) != 1 || router.calls != 1 {
		t.Fatalf("lookup = %v, %v, calls = %d", ips, err, router.calls)
	}
}

func TestMieruCloseStopsClient(t *testing.T) {
	var stopped bool
	client := &fakeClient{stop: func() error { stopped = true; return nil }}
	outbound := &Outbound{client: client}
	if err := outbound.Close(); err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("Close() did not stop the Mieru client")
	}
}

type fakeClient struct {
	mieruclient.Client
	stop func() error
}

func (c *fakeClient) Stop() error { return c.stop() }
