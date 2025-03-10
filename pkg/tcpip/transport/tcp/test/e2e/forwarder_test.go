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

package forwarder_test

import (
	"os"
	"testing"
	"time"

	"github.com/wilinz/gvisor/pkg/atomicbitops"
	"github.com/wilinz/gvisor/pkg/refs"
	"github.com/wilinz/gvisor/pkg/tcpip"
	"github.com/wilinz/gvisor/pkg/tcpip/checker"
	"github.com/wilinz/gvisor/pkg/tcpip/header"
	"github.com/wilinz/gvisor/pkg/tcpip/seqnum"
	"github.com/wilinz/gvisor/pkg/tcpip/transport/tcp"
	"github.com/wilinz/gvisor/pkg/tcpip/transport/tcp/test/e2e"
	"github.com/wilinz/gvisor/pkg/tcpip/transport/tcp/testing/context"
)

func TestForwarderSendMSSLessThanMTU(t *testing.T) {
	const maxPayload = 100
	const mtu = 1200
	c := context.New(t, mtu)
	defer c.Cleanup()

	s := c.Stack()
	ch := make(chan tcpip.Error, 1)
	f := tcp.NewForwarder(s, 65536, 10, func(r *tcp.ForwarderRequest) {
		var err tcpip.Error
		c.EP, err = r.CreateEndpoint(&c.WQ)
		ch <- err
		close(ch)
		r.Complete(false)
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, f.HandlePacket)

	// Do 3-way handshake.
	c.PassiveConnect(maxPayload, -1, header.TCPSynOptions{MSS: mtu - header.IPv4MinimumSize - header.TCPMinimumSize})

	// Wait for connection to be available.
	select {
	case err := <-ch:
		if err != nil {
			t.Fatalf("Error creating endpoint: %s", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Timed out waiting for connection")
	}

	// Check that data gets properly segmented.
	e2e.CheckBrokenUpWrite(t, c, maxPayload)
}

func TestForwarderDoesNotRejectECNFlags(t *testing.T) {
	testCases := []struct {
		name  string
		flags header.TCPFlags
	}{
		{name: "non-setup ECN SYN w/ ECE", flags: header.TCPFlagEce},
		{name: "non-setup ECN SYN w/ CWR", flags: header.TCPFlagCwr},
		{name: "setup ECN SYN", flags: header.TCPFlagEce | header.TCPFlagCwr},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			const maxPayload = 100
			const mtu = 1200
			c := context.New(t, mtu)
			defer c.Cleanup()

			s := c.Stack()
			ch := make(chan tcpip.Error, 1)
			f := tcp.NewForwarder(s, 65536, 10, func(r *tcp.ForwarderRequest) {
				var err tcpip.Error
				c.EP, err = r.CreateEndpoint(&c.WQ)
				ch <- err
				close(ch)
				r.Complete(false)
			})
			s.SetTransportProtocolHandler(tcp.ProtocolNumber, f.HandlePacket)

			// Do 3-way handshake.
			c.PassiveConnect(maxPayload, -1, header.TCPSynOptions{MSS: mtu - header.IPv4MinimumSize - header.TCPMinimumSize, Flags: tc.flags})

			// Wait for connection to be available.
			select {
			case err := <-ch:
				if err != nil {
					t.Fatalf("Error creating endpoint: %s", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("Timed out waiting for connection")
			}
		})
	}
}

func TestForwarderFailedConnect(t *testing.T) {
	const mtu = 1200
	c := context.New(t, mtu)
	defer c.Cleanup()

	s := c.Stack()
	ch := make(chan tcpip.Error, 1)
	f := tcp.NewForwarder(s, 65536, 10, func(r *tcp.ForwarderRequest) {
		var err tcpip.Error
		c.EP, err = r.CreateEndpoint(&c.WQ)
		ch <- err
		close(ch)
		r.Complete(false)
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, f.HandlePacket)

	// Initiate a connection that will be forwarded by the Forwarder.
	// Send a SYN request.
	iss := seqnum.Value(context.TestInitialSequenceNumber)
	c.SendPacket(nil, &context.Headers{
		SrcPort: context.TestPort,
		DstPort: context.StackPort,
		Flags:   header.TCPFlagSyn,
		SeqNum:  iss,
		RcvWnd:  30000,
	})

	// Receive the SYN-ACK reply. Make sure MSS and other expected options
	// are present.
	v := c.GetPacket()
	defer v.Release()
	tcp := header.TCP(header.IPv4(v.AsSlice()).Payload())
	c.IRS = seqnum.Value(tcp.SequenceNumber())

	tcpCheckers := []checker.TransportChecker{
		checker.SrcPort(context.StackPort),
		checker.DstPort(context.TestPort),
		checker.TCPFlags(header.TCPFlagAck | header.TCPFlagSyn),
		checker.TCPAckNum(uint32(iss) + 1),
	}
	checker.IPv4(t, v, checker.TCP(tcpCheckers...))

	// Now send an active RST to abort the handshake.
	c.SendPacket(nil, &context.Headers{
		SrcPort: context.TestPort,
		DstPort: context.StackPort,
		Flags:   header.TCPFlagRst,
		SeqNum:  iss + 1,
		RcvWnd:  0,
	})

	// Wait for connect to fail.
	select {
	case err := <-ch:
		if err == nil {
			t.Fatalf("endpoint creation should have failed")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Timed out waiting for connection to fail")
	}
}

func TestForwarderDroppedStats(t *testing.T) {
	const maxPayload = 100
	const mtu = 1200
	c := context.New(t, mtu)
	defer c.Cleanup()

	const maxInFlight = 2
	iters := atomicbitops.FromInt64(maxInFlight)
	s := c.Stack()
	checkedStats := make(chan struct{})
	done := make(chan struct{})
	f := tcp.NewForwarder(s, 65536, maxInFlight, func(r *tcp.ForwarderRequest) {
		<-checkedStats
		// Complete all requests without doing anything
		r.Complete(false)
		if iter := iters.Add(-1); iter == 0 {
			close(done)
		}
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, f.HandlePacket)

	for i := 0; i < maxInFlight+1; i++ {
		iss := seqnum.Value(context.TestInitialSequenceNumber + i)
		c.SendPacket(nil, &context.Headers{
			SrcPort: uint16(context.TestPort + i),
			DstPort: context.StackPort,
			Flags:   header.TCPFlagSyn,
			SeqNum:  iss,
			RcvWnd:  30000,
		})
	}

	// Verify that we got one ignored packet.
	if curr := s.Stats().TCP.ForwardMaxInFlightDrop.Value(); curr != 1 {
		t.Errorf("Expected one dropped connection, but got %d", curr)
	}
	close(checkedStats)
	<-done
}

func TestMain(m *testing.M) {
	refs.SetLeakMode(refs.LeaksPanic)
	code := m.Run()
	// Allow TCP async work to complete to avoid false reports of leaks.
	// TODO(gvisor.dev/issue/5940): Use fake clock in tests.
	time.Sleep(1 * time.Second)
	refs.DoLeakCheck()
	os.Exit(code)
}
