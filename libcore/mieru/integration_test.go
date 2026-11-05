package mieru

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	mierucommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/apis/server"
	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	"google.golang.org/protobuf/proto"
)

// Exercise the registered adapter against the official server over both wire transports.
func TestOfficialServerRoundTrip(t *testing.T) {
	for _, transport := range []string{"TCP", "UDP"} {
		t.Run(transport, func(t *testing.T) {
			probe, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := probe.Addr().(*net.TCPAddr).Port
			probe.Close()
			wire := pb.TransportProtocol_TCP
			if transport == "UDP" {
				wire = pb.TransportProtocol_UDP
			}
			srv := server.NewServer()
			if err := srv.Store(&server.ServerConfig{Config: &pb.ServerConfig{
				ListenIPAddress: proto.String("127.0.0.1"),
				PortBindings:    []*pb.PortBinding{{Port: proto.Int32(int32(port)), Protocol: wire.Enum()}},
				Users:           []*pb.User{{Name: proto.String("integration"), Password: proto.String("local-fixture-password")}},
			}}); err != nil {
				t.Fatal(err)
			}
			if err := srv.Start(); err != nil {
				t.Fatal(err)
			}
			defer srv.Stop()
			go func() {
				for {
					conn, request, err := srv.Accept()
					if err != nil {
						return
					}
					go func() {
						defer conn.Close()
						conn.SetDeadline(time.Now().Add(10 * time.Second))
						if err := model.WriteSocks5Response(conn, 0, model.AddrSpec{IP: net.IPv4(127, 0, 0, 1), Port: 1}); err != nil {
							return
						}
						if request.Command == 1 {
							io.Copy(conn, conn)
						} else {
							packets := mierucommon.NewUDPAssociateWrapper(mierucommon.NewPacketOverStreamTunnel(conn))
							data := make([]byte, 65535)
							for {
								n, address, err := packets.ReadFrom(data)
								if err != nil {
									return
								}
								if _, err := packets.WriteTo(data[:n], address); err != nil {
									return
								}
							}
						}
					}()
				}
			}()
			raw, err := NewOutbound(context.Background(), nil, log.NewNOPFactory().Logger(), "test", Options{
				ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: uint16(port)},
				Username:      "integration", Password: "local-fixture-password", Transport: transport,
			})
			if err != nil {
				t.Fatal(err)
			}
			outbound := raw.(*Outbound)
			defer outbound.Close()
			for _, network := range []string{"tcp", "udp"} {
				destination := M.ParseSocksaddr("203.0.113.10:18080")
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				conn, err := outbound.DialContext(ctx, network, destination)
				cancel()
				if err != nil {
					t.Fatalf("%s dial: %v", network, err)
				}
				conn.SetDeadline(time.Now().Add(10 * time.Second))
				payload := []byte("native-mieru-round-trip")
				if _, err := conn.Write(payload); err != nil {
					conn.Close()
					t.Fatal(err)
				}
				reply := make([]byte, len(payload))
				if _, err := io.ReadFull(conn, reply); err != nil {
					conn.Close()
					t.Fatal(err)
				}
				conn.Close()
				if string(reply) != string(payload) {
					t.Fatalf("%s payload = %q", network, reply)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			packets, err := outbound.ListenPacket(ctx, M.ParseSocksaddr("203.0.113.10:53"))
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			defer packets.Close()
			packets.SetDeadline(time.Now().Add(10 * time.Second))
			target := &net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 5353}
			payload := []byte("native-ipv6-datagram")
			if _, err := packets.WriteTo(payload, target); err != nil {
				t.Fatal(err)
			}
			reply := make([]byte, 128)
			n, address, err := packets.ReadFrom(reply)
			if err != nil {
				t.Fatal(err)
			}
			if string(reply[:n]) != string(payload) || address.String() != target.String() {
				t.Fatalf("UDP reply: %q %v", reply[:n], address)
			}
		})
	}
}
