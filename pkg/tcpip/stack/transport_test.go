// Copyright 2018 The gVisor Authors.
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

package stack_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/wilinz/gvisor/pkg/buffer"
	"github.com/wilinz/gvisor/pkg/tcpip"
	"github.com/wilinz/gvisor/pkg/tcpip/header"
	"github.com/wilinz/gvisor/pkg/tcpip/link/channel"
	"github.com/wilinz/gvisor/pkg/tcpip/ports"
	"github.com/wilinz/gvisor/pkg/tcpip/stack"
	"github.com/wilinz/gvisor/pkg/waiter"
)

const (
	fakeTransNumber    tcpip.TransportProtocolNumber = 1
	fakeTransHeaderLen int                           = 12
)

// fakeTransportEndpoint is a transport-layer protocol endpoint. It counts
// received packets; the counts of all endpoints are aggregated in the protocol
// descriptor.
//
// Headers of this protocol are fakeTransHeaderLen bytes.
type fakeTransportEndpoint struct {
	stack.TransportEndpointInfo
	tcpip.DefaultSocketOptionsHandler

	proto    *fakeTransportProtocol
	peerAddr tcpip.Address
	route    *stack.Route

	// acceptQueue is non-nil iff bound.
	acceptQueue []*fakeTransportEndpoint

	// ops is used to set and get socket options.
	ops tcpip.SocketOptions
}

func (f *fakeTransportEndpoint) Info() tcpip.EndpointInfo {
	return &f.TransportEndpointInfo
}

func (*fakeTransportEndpoint) Stats() tcpip.EndpointStats {
	return nil
}

func (*fakeTransportEndpoint) SetOwner(owner tcpip.PacketOwner) {}

func (f *fakeTransportEndpoint) SocketOptions() *tcpip.SocketOptions {
	return &f.ops
}

func newFakeTransportEndpoint(proto *fakeTransportProtocol, netProto tcpip.NetworkProtocolNumber, s *stack.Stack) tcpip.Endpoint {
	ep := &fakeTransportEndpoint{TransportEndpointInfo: stack.TransportEndpointInfo{NetProto: netProto}, proto: proto}
	ep.ops.InitHandler(ep, s, tcpip.GetStackSendBufferLimits, tcpip.GetStackReceiveBufferLimits)
	return ep
}

func (f *fakeTransportEndpoint) Abort() {
	f.Close()
}

func (*fakeTransportEndpoint) Release() {}

func (f *fakeTransportEndpoint) Close() {
	// TODO(gvisor.dev/issue/5153): Consider retaining the route.
	f.route.Release()
}

func (*fakeTransportEndpoint) Readiness(mask waiter.EventMask) waiter.EventMask {
	return mask
}

func (*fakeTransportEndpoint) Read(io.Writer, tcpip.ReadOptions) (tcpip.ReadResult, tcpip.Error) {
	return tcpip.ReadResult{}, nil
}

func (f *fakeTransportEndpoint) Write(p tcpip.Payloader, opts tcpip.WriteOptions) (int64, tcpip.Error) {
	if f.route.RemoteAddress().Len() == 0 {
		return 0, &tcpip.ErrHostUnreachable{}
	}

	v := make([]byte, p.Len())
	if _, err := io.ReadFull(p, v); err != nil {
		return 0, &tcpip.ErrBadBuffer{}
	}

	pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
		ReserveHeaderBytes: int(f.route.MaxHeaderLength()) + fakeTransHeaderLen,
		Payload:            buffer.MakeWithData(v),
	})
	_ = pkt.TransportHeader().Push(fakeTransHeaderLen)
	if err := f.route.WritePacket(stack.NetworkHeaderParams{Protocol: fakeTransNumber, TTL: 123, TOS: stack.DefaultTOS}, pkt); err != nil {
		return 0, err
	}

	return int64(len(v)), nil
}

// SetSockOpt sets a socket option. Currently not supported.
func (*fakeTransportEndpoint) SetSockOpt(tcpip.SettableSocketOption) tcpip.Error {
	return &tcpip.ErrInvalidEndpointState{}
}

// SetSockOptInt sets a socket option. Currently not supported.
func (*fakeTransportEndpoint) SetSockOptInt(tcpip.SockOptInt, int) tcpip.Error {
	return &tcpip.ErrInvalidEndpointState{}
}

// GetSockOptInt implements tcpip.Endpoint.GetSockOptInt.
func (*fakeTransportEndpoint) GetSockOptInt(opt tcpip.SockOptInt) (int, tcpip.Error) {
	return -1, &tcpip.ErrUnknownProtocolOption{}
}

// GetSockOpt implements tcpip.Endpoint.GetSockOpt.
func (*fakeTransportEndpoint) GetSockOpt(tcpip.GettableSocketOption) tcpip.Error {
	return &tcpip.ErrInvalidEndpointState{}
}

// Disconnect implements tcpip.Endpoint.Disconnect.
func (*fakeTransportEndpoint) Disconnect() tcpip.Error {
	return &tcpip.ErrNotSupported{}
}

func (f *fakeTransportEndpoint) Connect(addr tcpip.FullAddress) tcpip.Error {
	f.peerAddr = addr.Addr

	// Find the route.
	r, err := f.proto.stack.FindRoute(addr.NIC, tcpip.Address{}, addr.Addr, fakeNetNumber, false /* multicastLoop */)
	if err != nil {
		return &tcpip.ErrHostUnreachable{}
	}

	// Try to register so that we can start receiving packets.
	f.ID.RemoteAddress = addr.Addr
	err = f.proto.stack.RegisterTransportEndpoint([]tcpip.NetworkProtocolNumber{fakeNetNumber}, fakeTransNumber, f.ID, f, ports.Flags{}, 0 /* bindToDevice */)
	if err != nil {
		r.Release()
		return err
	}

	f.route = r

	return nil
}

func (*fakeTransportEndpoint) ConnectEndpoint(e tcpip.Endpoint) tcpip.Error {
	return nil
}

func (*fakeTransportEndpoint) Shutdown(tcpip.ShutdownFlags) tcpip.Error {
	return nil
}

func (*fakeTransportEndpoint) Reset() {
}

func (*fakeTransportEndpoint) Listen(int) tcpip.Error {
	return nil
}

func (f *fakeTransportEndpoint) Accept(*tcpip.FullAddress) (tcpip.Endpoint, *waiter.Queue, tcpip.Error) {
	if len(f.acceptQueue) == 0 {
		return nil, nil, nil
	}
	a := f.acceptQueue[0]
	f.acceptQueue = f.acceptQueue[1:]
	return a, nil, nil
}

func (f *fakeTransportEndpoint) Bind(a tcpip.FullAddress) tcpip.Error {
	if err := f.proto.stack.RegisterTransportEndpoint(
		[]tcpip.NetworkProtocolNumber{fakeNetNumber},
		fakeTransNumber,
		stack.TransportEndpointID{LocalAddress: a.Addr},
		f,
		ports.Flags{},
		0, /* bindtoDevice */
	); err != nil {
		return err
	}
	f.acceptQueue = []*fakeTransportEndpoint{}
	return nil
}

func (*fakeTransportEndpoint) GetLocalAddress() (tcpip.FullAddress, tcpip.Error) {
	return tcpip.FullAddress{}, nil
}

func (*fakeTransportEndpoint) GetRemoteAddress() (tcpip.FullAddress, tcpip.Error) {
	return tcpip.FullAddress{}, nil
}

func (f *fakeTransportEndpoint) HandlePacket(id stack.TransportEndpointID, pkt *stack.PacketBuffer) {
	// Increment the number of received packets.
	f.proto.packetCount++
	if f.acceptQueue == nil {
		return
	}

	netHdr := pkt.NetworkHeader().Slice()
	route, err := f.proto.stack.FindRoute(
		pkt.NICID,
		tcpip.AddrFromSlice(netHdr[dstAddrOffset:][:header.IPv4AddressSize]),
		tcpip.AddrFromSlice(netHdr[srcAddrOffset:][:header.IPv4AddressSize]),
		pkt.NetworkProtocolNumber,
		false /* multicastLoop */)
	if err != nil {
		return
	}

	ep := &fakeTransportEndpoint{
		TransportEndpointInfo: stack.TransportEndpointInfo{
			ID:       f.ID,
			NetProto: f.NetProto,
		},
		proto:    f.proto,
		peerAddr: route.RemoteAddress(),
		route:    route,
	}
	ep.ops.InitHandler(ep, f.proto.stack, tcpip.GetStackSendBufferLimits, tcpip.GetStackReceiveBufferLimits)
	f.acceptQueue = append(f.acceptQueue, ep)
}

func (f *fakeTransportEndpoint) HandleError(stack.TransportError, *stack.PacketBuffer) {
	// Increment the number of received control packets.
	f.proto.controlCount++
}

func (*fakeTransportEndpoint) State() uint32 {
	return 0
}

func (*fakeTransportEndpoint) ModerateRecvBuf(copied int) {}

func (*fakeTransportEndpoint) Restore(*stack.Stack) {}

func (*fakeTransportEndpoint) Wait() {}

func (*fakeTransportEndpoint) LastError() tcpip.Error {
	return nil
}

type fakeTransportGoodOption bool

type fakeTransportBadOption bool

type fakeTransportInvalidValueOption int

type fakeTransportProtocolOptions struct {
	good bool
}

// fakeTransportProtocol is a transport-layer protocol descriptor. It
// aggregates the number of packets received via endpoints of this protocol.
type fakeTransportProtocol struct {
	stack *stack.Stack

	packetCount  int
	controlCount int
	opts         fakeTransportProtocolOptions
}

func (*fakeTransportProtocol) Number() tcpip.TransportProtocolNumber {
	return fakeTransNumber
}

func (f *fakeTransportProtocol) NewEndpoint(netProto tcpip.NetworkProtocolNumber, _ *waiter.Queue) (tcpip.Endpoint, tcpip.Error) {
	return newFakeTransportEndpoint(f, netProto, f.stack), nil
}

func (*fakeTransportProtocol) NewRawEndpoint(tcpip.NetworkProtocolNumber, *waiter.Queue) (tcpip.Endpoint, tcpip.Error) {
	return nil, &tcpip.ErrUnknownProtocol{}
}

func (*fakeTransportProtocol) MinimumPacketSize() int {
	return fakeTransHeaderLen
}

func (*fakeTransportProtocol) ParsePorts([]byte) (src, dst uint16, err tcpip.Error) {
	return 0, 0, nil
}

func (*fakeTransportProtocol) HandleUnknownDestinationPacket(stack.TransportEndpointID, *stack.PacketBuffer) stack.UnknownDestinationPacketDisposition {
	return stack.UnknownDestinationPacketHandled
}

func (f *fakeTransportProtocol) SetOption(option tcpip.SettableTransportProtocolOption) tcpip.Error {
	switch v := option.(type) {
	case *tcpip.TCPModerateReceiveBufferOption:
		f.opts.good = bool(*v)
		return nil
	default:
		return &tcpip.ErrUnknownProtocolOption{}
	}
}

func (f *fakeTransportProtocol) Option(option tcpip.GettableTransportProtocolOption) tcpip.Error {
	switch v := option.(type) {
	case *tcpip.TCPModerateReceiveBufferOption:
		*v = tcpip.TCPModerateReceiveBufferOption(f.opts.good)
		return nil
	default:
		return &tcpip.ErrUnknownProtocolOption{}
	}
}

// Abort implements TransportProtocol.Abort.
func (*fakeTransportProtocol) Abort() {}

// Close implements tcpip.Endpoint.Close.
func (*fakeTransportProtocol) Close() {}

// Wait implements TransportProtocol.Wait.
func (*fakeTransportProtocol) Wait() {}

// Pause implements TransportProtocol.Pause.
func (*fakeTransportProtocol) Pause() {}

// Resume implements TransportProtocol.Resume.
func (*fakeTransportProtocol) Resume() {}

// Restore implements TransportProtocol.Restore.
func (*fakeTransportProtocol) Restore() {}

// Parse implements TransportProtocol.Parse.
func (*fakeTransportProtocol) Parse(pkt *stack.PacketBuffer) bool {
	if _, ok := pkt.TransportHeader().Consume(fakeTransHeaderLen); ok {
		pkt.TransportProtocolNumber = fakeTransNumber
		return true
	}
	return false
}

func fakeTransFactory(s *stack.Stack) stack.TransportProtocol {
	return &fakeTransportProtocol{stack: s}
}

func TestTransportReceive(t *testing.T) {
	linkEP := channel.New(10, defaultMTU, "")
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{fakeNetFactory},
		TransportProtocols: []stack.TransportProtocolFactory{fakeTransFactory},
	})
	if err := s.CreateNIC(1, linkEP); err != nil {
		t.Fatalf("CreateNIC failed: %v", err)
	}

	{
		subnet, err := tcpip.NewSubnet(tcpip.AddrFromSlice([]byte("\x00\x00\x00\x00")), tcpip.MaskFrom("\x00\x00\x00\x00"))
		if err != nil {
			t.Fatal(err)
		}
		s.SetRouteTable([]tcpip.Route{{Destination: subnet, Gateway: tcpip.AddrFromSlice([]byte("\x00\x00\x00\x00")), NIC: 1}})
	}

	protocolAddr := tcpip.ProtocolAddress{
		Protocol: fakeNetNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   tcpip.AddrFromSlice([]byte("\x01\x00\x00\x00")),
			PrefixLen: fakeDefaultPrefixLen,
		},
	}
	if err := s.AddProtocolAddress(1, protocolAddr, stack.AddressProperties{}); err != nil {
		t.Fatalf("AddProtocolAddress(%d, %+v, {}): %s", 1, protocolAddr, err)
	}

	// Create endpoint and connect to remote address.
	wq := waiter.Queue{}
	ep, err := s.NewEndpoint(fakeTransNumber, fakeNetNumber, &wq)
	if err != nil {
		t.Fatalf("NewEndpoint failed: %v", err)
	}

	if err := ep.Connect(tcpip.FullAddress{Addr: tcpip.AddrFromSlice([]byte("\x02\x00\x00\x00"))}); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	fakeTrans := s.TransportProtocolInstance(fakeTransNumber).(*fakeTransportProtocol)

	// Create buffer that will hold the packet.
	buf := make([]byte, 30)

	// Make sure packet with wrong protocol is not delivered.
	copy(buf[dstAddrOffset:], []byte("\x01\x00\x00\x00"))
	buf[protocolNumberOffset] = 0
	linkEP.InjectInbound(fakeNetNumber, stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buffer.MakeWithData(buf),
	}))
	if fakeTrans.packetCount != 0 {
		t.Errorf("packetCount = %d, want %d", fakeTrans.packetCount, 0)
	}

	// Make sure packet from the wrong source is not delivered.
	copy(buf[dstAddrOffset:], []byte("\x01\x00\x00\x00"))
	copy(buf[srcAddrOffset:], []byte("\x03\x00\x00\x00"))
	buf[protocolNumberOffset] = byte(fakeTransNumber)
	linkEP.InjectInbound(fakeNetNumber, stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buffer.MakeWithData(buf),
	}))
	if fakeTrans.packetCount != 0 {
		t.Errorf("packetCount = %d, want %d", fakeTrans.packetCount, 0)
	}

	// Make sure packet is delivered.
	copy(buf[dstAddrOffset:], []byte("\x01\x00\x00\x00"))
	copy(buf[srcAddrOffset:], []byte("\x02\x00\x00\x00"))
	buf[protocolNumberOffset] = byte(fakeTransNumber)
	linkEP.InjectInbound(fakeNetNumber, stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buffer.MakeWithData(buf),
	}))
	if fakeTrans.packetCount != 1 {
		t.Errorf("packetCount = %d, want %d", fakeTrans.packetCount, 1)
	}
}

func TestTransportControlReceive(t *testing.T) {
	linkEP := channel.New(10, defaultMTU, "")
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{fakeNetFactory},
		TransportProtocols: []stack.TransportProtocolFactory{fakeTransFactory},
	})
	if err := s.CreateNIC(1, linkEP); err != nil {
		t.Fatalf("CreateNIC failed: %v", err)
	}

	{
		subnet, err := tcpip.NewSubnet(tcpip.AddrFromSlice([]byte("\x00\x00\x00\x00")), tcpip.MaskFrom("\x00\x00\x00\x00"))
		if err != nil {
			t.Fatal(err)
		}
		s.SetRouteTable([]tcpip.Route{{Destination: subnet, Gateway: tcpip.AddrFromSlice([]byte("\x00\x00\x00\x00")), NIC: 1}})
	}

	protocolAddr := tcpip.ProtocolAddress{
		Protocol: fakeNetNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   tcpip.AddrFromSlice([]byte("\x01\x00\x00\x00")),
			PrefixLen: fakeDefaultPrefixLen,
		},
	}
	if err := s.AddProtocolAddress(1, protocolAddr, stack.AddressProperties{}); err != nil {
		t.Fatalf("AddProtocolAddress(%d, %+v, {}): %s", 1, protocolAddr, err)
	}

	// Create endpoint and connect to remote address.
	wq := waiter.Queue{}
	ep, err := s.NewEndpoint(fakeTransNumber, fakeNetNumber, &wq)
	if err != nil {
		t.Fatalf("NewEndpoint failed: %v", err)
	}

	if err := ep.Connect(tcpip.FullAddress{Addr: tcpip.AddrFromSlice([]byte("\x02\x00\x00\x00"))}); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	fakeTrans := s.TransportProtocolInstance(fakeTransNumber).(*fakeTransportProtocol)

	// Create buffer that will hold the control packet.
	buf := make([]byte, 2*fakeNetHeaderLen+30)

	// Outer packet contains the control protocol number.
	copy(buf[dstAddrOffset:], []byte("\x01\x00\x00\x00"))
	copy(buf[srcAddrOffset:], []byte("\xfe\x00\x00\x00"))
	buf[protocolNumberOffset] = byte(fakeControlProtocol)

	// Make sure packet with wrong protocol is not delivered.
	copy(buf[fakeNetHeaderLen:][dstAddrOffset:], []byte("\x00\x00\x00\x00"))
	copy(buf[fakeNetHeaderLen:][srcAddrOffset:], []byte("\x01\x00\x00\x00"))
	buf[fakeNetHeaderLen:][protocolNumberOffset] = 0
	linkEP.InjectInbound(fakeNetNumber, stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buffer.MakeWithData(buf),
	}))
	if fakeTrans.controlCount != 0 {
		t.Errorf("controlCount = %d, want %d", fakeTrans.controlCount, 0)
	}

	// Make sure packet from the wrong source is not delivered.
	copy(buf[fakeNetHeaderLen:][dstAddrOffset:], []byte("\x03\x00\x00\x00"))
	copy(buf[fakeNetHeaderLen:][srcAddrOffset:], []byte("\x01\x00\x00\x00"))
	buf[fakeNetHeaderLen:][protocolNumberOffset] = byte(fakeTransNumber)
	linkEP.InjectInbound(fakeNetNumber, stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buffer.MakeWithData(buf),
	}))
	if fakeTrans.controlCount != 0 {
		t.Errorf("controlCount = %d, want %d", fakeTrans.controlCount, 0)
	}

	// Make sure packet is delivered.
	copy(buf[fakeNetHeaderLen:][dstAddrOffset:], []byte("\x02\x00\x00\x00"))
	copy(buf[fakeNetHeaderLen:][srcAddrOffset:], []byte("\x01\x00\x00\x00"))
	buf[fakeNetHeaderLen:][protocolNumberOffset] = byte(fakeTransNumber)
	linkEP.InjectInbound(fakeNetNumber, stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buffer.MakeWithData(buf),
	}))
	if fakeTrans.controlCount != 1 {
		t.Errorf("controlCount = %d, want %d", fakeTrans.controlCount, 1)
	}
}

func TestTransportSend(t *testing.T) {
	linkEP := channel.New(10, defaultMTU, "")
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{fakeNetFactory},
		TransportProtocols: []stack.TransportProtocolFactory{fakeTransFactory},
	})
	if err := s.CreateNIC(1, linkEP); err != nil {
		t.Fatalf("CreateNIC failed: %v", err)
	}

	protocolAddr := tcpip.ProtocolAddress{
		Protocol: fakeNetNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   tcpip.AddrFromSlice([]byte("\x01\x00\x00\x00")),
			PrefixLen: fakeDefaultPrefixLen,
		},
	}
	if err := s.AddProtocolAddress(1, protocolAddr, stack.AddressProperties{}); err != nil {
		t.Fatalf("AddProtocolAddress(%d, %+v, {}): %s", 1, protocolAddr, err)
	}

	{
		subnet, err := tcpip.NewSubnet(tcpip.AddrFromSlice([]byte("\x00\x00\x00\x00")), tcpip.MaskFrom("\x00\x00\x00\x00"))
		if err != nil {
			t.Fatal(err)
		}
		s.SetRouteTable([]tcpip.Route{{Destination: subnet, Gateway: tcpip.AddrFromSlice([]byte("\x00\x00\x00\x00")), NIC: 1}})
	}

	// Create endpoint and bind it.
	wq := waiter.Queue{}
	ep, err := s.NewEndpoint(fakeTransNumber, fakeNetNumber, &wq)
	if err != nil {
		t.Fatalf("NewEndpoint failed: %v", err)
	}

	if err := ep.Connect(tcpip.FullAddress{Addr: tcpip.AddrFromSlice([]byte("\x02\x00\x00\x00"))}); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Create buffer that will hold the payload.
	b := make([]byte, 30)
	var r bytes.Reader
	r.Reset(b)
	if _, err := ep.Write(&r, tcpip.WriteOptions{}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	fakeNet := s.NetworkProtocolInstance(fakeNetNumber).(*fakeNetworkProtocol)

	if fakeNet.sendPacketCount[2] != 1 {
		t.Errorf("sendPacketCount = %d, want %d", fakeNet.sendPacketCount[2], 1)
	}
}

func TestTransportOptions(t *testing.T) {
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{fakeNetFactory},
		TransportProtocols: []stack.TransportProtocolFactory{fakeTransFactory},
	})

	v := tcpip.TCPModerateReceiveBufferOption(true)
	if err := s.SetTransportProtocolOption(fakeTransNumber, &v); err != nil {
		t.Errorf("s.SetTransportProtocolOption(fakeTrans, &%T(%t)): %s", v, v, err)
	}
	v = false
	if err := s.TransportProtocolOption(fakeTransNumber, &v); err != nil {
		t.Fatalf("s.TransportProtocolOption(fakeTransNumber, &%T): %s", v, err)
	}
	if !v {
		t.Fatalf("got tcpip.TCPModerateReceiveBufferOption = false, want = true")
	}
}
