// Copyright 2020 The gVisor Authors.
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

package message_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/wilinz/gvisor/pkg/abi/linux"
	"github.com/wilinz/gvisor/pkg/marshal"
	"github.com/wilinz/gvisor/pkg/marshal/primitive"
	"github.com/wilinz/gvisor/pkg/sentry/socket/netlink/nlmsg"
)

func TestParseMessage(t *testing.T) {
	dummyNetlinkMsg := primitive.Uint16(0x3130)
	tests := []struct {
		desc  string
		input []byte

		header  linux.NetlinkMessageHeader
		dataMsg marshal.Marshallable
		restLen int
		ok      bool
	}{
		{
			desc: "valid",
			input: []byte{
				0x14, 0x00, 0x00, 0x00, // Length
				0x01, 0x00, // Type
				0x02, 0x00, // Flags
				0x03, 0x00, 0x00, 0x00, // Seq
				0x04, 0x00, 0x00, 0x00, // PortID
				0x30, 0x31, 0x00, 0x00, // Data message with 2 bytes padding
			},
			header: linux.NetlinkMessageHeader{
				Length: 20,
				Type:   1,
				Flags:  2,
				Seq:    3,
				PortID: 4,
			},
			dataMsg: &dummyNetlinkMsg,
			restLen: 0,
			ok:      true,
		},
		{
			desc: "valid with next message",
			input: []byte{
				0x14, 0x00, 0x00, 0x00, // Length
				0x01, 0x00, // Type
				0x02, 0x00, // Flags
				0x03, 0x00, 0x00, 0x00, // Seq
				0x04, 0x00, 0x00, 0x00, // PortID
				0x30, 0x31, 0x00, 0x00, // Data message with 2 bytes padding
				0xFF, // Next message (rest)
			},
			header: linux.NetlinkMessageHeader{
				Length: 20,
				Type:   1,
				Flags:  2,
				Seq:    3,
				PortID: 4,
			},
			dataMsg: &dummyNetlinkMsg,
			restLen: 1,
			ok:      true,
		},
		{
			desc: "valid for last message without padding",
			input: []byte{
				0x12, 0x00, 0x00, 0x00, // Length
				0x01, 0x00, // Type
				0x02, 0x00, // Flags
				0x03, 0x00, 0x00, 0x00, // Seq
				0x04, 0x00, 0x00, 0x00, // PortID
				0x30, 0x31, // Data message
			},
			header: linux.NetlinkMessageHeader{
				Length: 18,
				Type:   1,
				Flags:  2,
				Seq:    3,
				PortID: 4,
			},
			dataMsg: &dummyNetlinkMsg,
			restLen: 0,
			ok:      true,
		},
		{
			desc: "valid for last message not to be aligned",
			input: []byte{
				0x13, 0x00, 0x00, 0x00, // Length
				0x01, 0x00, // Type
				0x02, 0x00, // Flags
				0x03, 0x00, 0x00, 0x00, // Seq
				0x04, 0x00, 0x00, 0x00, // PortID
				0x30, 0x31, // Data message
				0x00, // Excessive 1 byte permitted at end
			},
			header: linux.NetlinkMessageHeader{
				Length: 19,
				Type:   1,
				Flags:  2,
				Seq:    3,
				PortID: 4,
			},
			dataMsg: &dummyNetlinkMsg,
			restLen: 0,
			ok:      true,
		},
		{
			desc: "header.Length too short",
			input: []byte{
				0x04, 0x00, 0x00, 0x00, // Length
				0x01, 0x00, // Type
				0x02, 0x00, // Flags
				0x03, 0x00, 0x00, 0x00, // Seq
				0x04, 0x00, 0x00, 0x00, // PortID
				0x30, 0x31, 0x00, 0x00, // Data message with 2 bytes padding
			},
			ok: false,
		},
		{
			desc: "header.Length too long",
			input: []byte{
				0xFF, 0xFF, 0x00, 0x00, // Length
				0x01, 0x00, // Type
				0x02, 0x00, // Flags
				0x03, 0x00, 0x00, 0x00, // Seq
				0x04, 0x00, 0x00, 0x00, // PortID
				0x30, 0x31, 0x00, 0x00, // Data message with 2 bytes padding
			},
			ok: false,
		},
		{
			desc: "header incomplete",
			input: []byte{
				0x04, 0x00, 0x00, 0x00, // Length
			},
			ok: false,
		},
		{
			desc:  "empty message",
			input: []byte{},
			ok:    false,
		},
	}
	for _, test := range tests {
		msg, rest, ok := nlmsg.ParseMessage(test.input)
		if ok != test.ok {
			t.Errorf("%v: got ok = %v, want = %v", test.desc, ok, test.ok)
			continue
		}
		if !test.ok {
			continue
		}
		if !reflect.DeepEqual(msg.Header(), test.header) {
			t.Errorf("%v: got hdr = %+v, want = %+v", test.desc, msg.Header(), test.header)
		}

		var dataMsg primitive.Uint16
		_, dataOk := msg.GetData(&dataMsg)
		if !dataOk {
			t.Errorf("%v: GetData.ok = %v, want = true", test.desc, dataOk)
		} else if !reflect.DeepEqual(&dataMsg, test.dataMsg) {
			t.Errorf("%v: GetData.msg = %+v, want = %+v", test.desc, dataMsg, test.dataMsg)
		}

		if got, want := rest, test.input[len(test.input)-test.restLen:]; !bytes.Equal(got, want) {
			t.Errorf("%v: got rest = %v, want = %v", test.desc, got, want)
		}
	}
}

func TestAttrView(t *testing.T) {
	tests := []struct {
		desc  string
		input []byte

		// Outputs for ParseFirst.
		hdr     linux.NetlinkAttrHeader
		value   []byte
		restLen int
		ok      bool

		// Outputs for Empty.
		isEmpty bool
	}{
		{
			desc: "valid",
			input: []byte{
				0x06, 0x00, // Length
				0x01, 0x00, // Type
				0x30, 0x31, 0x00, 0x00, // Data with 2 bytes padding
			},
			hdr: linux.NetlinkAttrHeader{
				Length: 6,
				Type:   1,
			},
			value:   []byte{0x30, 0x31},
			restLen: 0,
			ok:      true,
			isEmpty: false,
		},
		{
			desc: "at alignment",
			input: []byte{
				0x08, 0x00, // Length
				0x01, 0x00, // Type
				0x30, 0x31, 0x32, 0x33, // Data
			},
			hdr: linux.NetlinkAttrHeader{
				Length: 8,
				Type:   1,
			},
			value:   []byte{0x30, 0x31, 0x32, 0x33},
			restLen: 0,
			ok:      true,
			isEmpty: false,
		},
		{
			desc: "at alignment with rest data",
			input: []byte{
				0x08, 0x00, // Length
				0x01, 0x00, // Type
				0x30, 0x31, 0x32, 0x33, // Data
				0xFF, 0xFE, // Rest data
			},
			hdr: linux.NetlinkAttrHeader{
				Length: 8,
				Type:   1,
			},
			value:   []byte{0x30, 0x31, 0x32, 0x33},
			restLen: 2,
			ok:      true,
			isEmpty: false,
		},
		{
			desc: "hdr.Length too long",
			input: []byte{
				0xFF, 0x00, // Length
				0x01, 0x00, // Type
				0x30, 0x31, 0x32, 0x33, // Data
			},
			ok:      false,
			isEmpty: false,
		},
		{
			desc: "hdr.Length too short",
			input: []byte{
				0x01, 0x00, // Length
				0x01, 0x00, // Type
				0x30, 0x31, 0x32, 0x33, // Data
			},
			ok:      false,
			isEmpty: false,
		},
		{
			desc:    "empty",
			input:   []byte{},
			ok:      false,
			isEmpty: true,
		},
	}
	for _, test := range tests {
		attrs := nlmsg.AttrsView(test.input)

		// Test ParseFirst().
		hdr, value, rest, ok := attrs.ParseFirst()
		if ok != test.ok {
			t.Errorf("%v: got ok = %v, want = %v", test.desc, ok, test.ok)
		} else if test.ok {
			if !reflect.DeepEqual(hdr, test.hdr) {
				t.Errorf("%v: got hdr = %+v, want = %+v", test.desc, hdr, test.hdr)
			}
			if !bytes.Equal(value, test.value) {
				t.Errorf("%v: got value = %v, want = %v", test.desc, value, test.value)
			}
			if wantRest := test.input[len(test.input)-test.restLen:]; !bytes.Equal(rest, wantRest) {
				t.Errorf("%v: got rest = %v, want = %v", test.desc, rest, wantRest)
			}
		}

		// Test Empty().
		if got, want := attrs.Empty(), test.isEmpty; got != want {
			t.Errorf("%v: got empty = %v, want = %v", test.desc, got, want)
		}
	}
}

type bytesViewTest[T any] struct {
	desc  string
	input nlmsg.BytesView
	ok    bool
	value T
}

func TestBytesView(t *testing.T) {
	tests := []any{
		bytesViewTest[string]{
			desc:  "Convert BytesView to string",
			input: nlmsg.BytesView([]byte("hello world")),
			ok:    true,
			value: "hello world",
		},
		bytesViewTest[uint32]{
			desc:  "Convert BytesView to uint32",
			input: nlmsg.BytesView([]byte{7, 0, 0, 0}),
			ok:    true,
			value: 7,
		},
		bytesViewTest[uint32]{
			desc:  "Failed to convert BytesView to uint32",
			input: nlmsg.BytesView([]byte{7, 0}),
			ok:    false,
			value: 0,
		},
		bytesViewTest[int32]{
			desc:  "Convert BytesView to int32",
			input: nlmsg.BytesView([]byte{8, 0, 0, 0}),
			ok:    true,
			value: 8,
		},
		bytesViewTest[int32]{
			desc:  "Failed convert BytesView to int32",
			input: nlmsg.BytesView([]byte{8}),
			ok:    false,
			value: 0,
		},
	}
	for _, test := range tests {
		switch test.(type) {
		case bytesViewTest[string]:
			tst := test.(bytesViewTest[string])
			value := tst.input.String()
			if value != tst.value {
				t.Errorf("%v: BytesView.String() got %v, want %v", tst.desc, value, tst.value)
			}
		case bytesViewTest[uint32]:
			tst := test.(bytesViewTest[uint32])
			value, ok := tst.input.Uint32()
			if ok != tst.ok {
				t.Errorf("%v: BytesView.Uint32() got ok = %v, want %v", tst.desc, ok, tst.ok)
			}
			if ok && value != tst.value {
				t.Errorf("%v: BytesView.Uint32() got %v, want %v", tst.desc, value, tst.value)
			}
		case bytesViewTest[int32]:
			tst := test.(bytesViewTest[int32])
			value, ok := tst.input.Int32()
			if ok != tst.ok {
				t.Errorf("%v: BytesView.Int32() got ok = %v, want %v", tst.desc, ok, tst.ok)
			}
			if ok && value != tst.value {
				t.Errorf("%v: BytesView.Int32() got %v, want %v", tst.desc, value, tst.value)
			}
		default:
			t.Errorf("BytesView %T not support", t)
		}
	}
}
