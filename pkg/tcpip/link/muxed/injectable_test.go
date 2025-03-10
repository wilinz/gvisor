// Copyright 2019 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package muxed

import (
	"bytes"
	"net"
	"os"
	"testing"

	"golang.org/x/sys/unix"
	"github.com/wilinz/gvisor/pkg/buffer"
	"github.com/wilinz/gvisor/pkg/refs"
	"github.com/wilinz/gvisor/pkg/tcpip"
	"github.com/wilinz/gvisor/pkg/tcpip/link/fdbased"
	"github.com/wilinz/gvisor/pkg/tcpip/network/ipv4"
	"github.com/wilinz/gvisor/pkg/tcpip/stack"
)

func TestInjectableEndpointMTU(t *testing.T) {
	endpoint, _, _ := makeTestInjectableEndpoint(t)

	mtus := []uint32{100, 200}
	for _, mtu := range mtus {
		endpoint.SetMTU(mtu)

		if want, v := mtu, endpoint.MTU(); want != v {
			t.Errorf("MTU() = %v, want %v", v, want)
		}
	}
}

func TestInjectableEndpointRawDispatch(t *testing.T) {
	endpoint, sock, dstIP := makeTestInjectableEndpoint(t)

	v := buffer.NewViewWithData([]byte{0xFA})
	defer v.Release()
	endpoint.InjectOutbound(dstIP, v)

	buf := make([]byte, ipv4.MaxTotalSize)
	bytesRead, err := sock.Read(buf)
	if err != nil {
		t.Fatalf("Unable to read from socketpair: %v", err)
	}
	if got, want := buf[:bytesRead], []byte{0xFA}; !bytes.Equal(got, want) {
		t.Fatalf("Read %v from the socketpair, wanted %v", got, want)
	}
}

func TestInjectableEndpointDispatch(t *testing.T) {
	endpoint, sock, dstIP := makeTestInjectableEndpoint(t)

	pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
		ReserveHeaderBytes: 1,
		Payload:            buffer.MakeWithData([]byte{0xFB}),
	})
	defer pkt.DecRef()
	pkt.TransportHeader().Push(1)[0] = 0xFA
	pkt.EgressRoute.RemoteAddress = dstIP
	pkt.NetworkProtocolNumber = ipv4.ProtocolNumber

	var pkts stack.PacketBufferList
	pkts.PushBack(pkt)
	if _, err := endpoint.WritePackets(pkts); err != nil {
		t.Fatalf("Unable to write packets: %s", err)
	}

	buf := make([]byte, 6500)
	bytesRead, err := sock.Read(buf)
	if err != nil {
		t.Fatalf("Unable to read from socketpair: %v", err)
	}
	if got, want := buf[:bytesRead], []byte{0xFA, 0xFB}; !bytes.Equal(got, want) {
		t.Fatalf("Read %v from the socketpair, wanted %v", got, want)
	}
}

func TestInjectableEndpointDispatchHdrOnly(t *testing.T) {
	endpoint, sock, dstIP := makeTestInjectableEndpoint(t)

	pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
		ReserveHeaderBytes: 1,
	})
	defer pkt.DecRef()
	pkt.TransportHeader().Push(1)[0] = 0xFA
	pkt.EgressRoute.RemoteAddress = dstIP
	pkt.NetworkProtocolNumber = ipv4.ProtocolNumber

	var pkts stack.PacketBufferList
	pkts.PushBack(pkt)
	if _, err := endpoint.WritePackets(pkts); err != nil {
		t.Fatalf("Unable to write packets: %s", err)
	}
	buf := make([]byte, 6500)
	bytesRead, err := sock.Read(buf)
	if err != nil {
		t.Fatalf("Unable to read from socketpair: %v", err)
	}
	if got, want := buf[:bytesRead], []byte{0xFA}; !bytes.Equal(got, want) {
		t.Fatalf("Read %v from the socketpair, wanted %v", got, want)
	}
}

func makeTestInjectableEndpoint(t *testing.T) (*InjectableEndpoint, *os.File, tcpip.Address) {
	dstIP := tcpip.AddrFromSlice(net.ParseIP("1.2.3.4").To4())
	pair, err := unix.Socketpair(unix.AF_UNIX,
		unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatal("Failed to create socket pair:", err)
	}
	underlyingEndpoint, err := fdbased.NewInjectable(pair[1], 6500, stack.CapabilityNone)
	if err != nil {
		t.Fatalf("fdbased.NewInjectable(%d, 6500, stack.CapabilityNone) failed: %s", pair[1], err)
	}
	routes := map[tcpip.Address]stack.InjectableLinkEndpoint{dstIP: underlyingEndpoint}
	endpoint := NewInjectableEndpoint(routes)
	return endpoint, os.NewFile(uintptr(pair[0]), "test route end"), dstIP
}

func TestMain(m *testing.M) {
	refs.SetLeakMode(refs.LeaksPanic)
	code := m.Run()
	refs.DoLeakCheck()
	os.Exit(code)
}
