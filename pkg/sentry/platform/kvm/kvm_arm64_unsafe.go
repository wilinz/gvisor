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

//go:build arm64
// +build arm64

package kvm

import (
	"fmt"

	"golang.org/x/sys/unix"
	"github.com/wilinz/gvisor/pkg/hostsyscall"
)

var (
	runDataSize  int
	hasGuestPCID bool
)

func updateSystemValues(fd int) error {
	// Extract the mmap size.
	sz, errno := hostsyscall.RawSyscall(unix.SYS_IOCTL, uintptr(fd), KVM_GET_VCPU_MMAP_SIZE, 0)
	if errno != 0 {
		return fmt.Errorf("getting VCPU mmap size: %v", errno)
	}
	// Save the data.
	runDataSize = int(sz)
	hasGuestPCID = true

	// Success.
	return nil
}
