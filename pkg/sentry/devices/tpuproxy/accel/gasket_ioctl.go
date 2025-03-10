// Copyright 2023 The gVisor Authors.
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

package accel

import (
	"golang.org/x/sys/unix"
	"github.com/wilinz/gvisor/pkg/abi/gasket"
	"github.com/wilinz/gvisor/pkg/abi/linux"
	"github.com/wilinz/gvisor/pkg/abi/tpu"
	"github.com/wilinz/gvisor/pkg/cleanup"
	"github.com/wilinz/gvisor/pkg/context"
	"github.com/wilinz/gvisor/pkg/errors/linuxerr"
	"github.com/wilinz/gvisor/pkg/hostarch"
	"github.com/wilinz/gvisor/pkg/sentry/devices/tpuproxy/util"
	"github.com/wilinz/gvisor/pkg/sentry/fsimpl/eventfd"
	"github.com/wilinz/gvisor/pkg/sentry/kernel"
	"github.com/wilinz/gvisor/pkg/sentry/memmap"
	"github.com/wilinz/gvisor/pkg/sentry/mm"
)

func gasketMapBufferIoctl(ctx context.Context, t *kernel.Task, hostFd int32, fd *accelFD, paramsAddr hostarch.Addr) (uintptr, error) {
	var userIoctlParams gasket.GasketPageTableIoctl
	if _, err := userIoctlParams.CopyIn(t, paramsAddr); err != nil {
		return 0, err
	}

	numberOfPageTables := tpu.NumberOfTPUV4PageTables
	if fd.device.lite {
		numberOfPageTables = tpu.NumberOfTPUV4litePageTables
	}
	if userIoctlParams.PageTableIndex >= numberOfPageTables {
		return 0, linuxerr.EFAULT
	}

	tmm := t.MemoryManager()
	ar, ok := tmm.CheckIORange(hostarch.Addr(userIoctlParams.HostAddress), int64(userIoctlParams.Size))
	if !ok {
		return 0, linuxerr.EFAULT
	}

	if !ar.IsPageAligned() || (userIoctlParams.Size/hostarch.PageSize) == 0 {
		return 0, linuxerr.EINVAL
	}

	devAddr := userIoctlParams.DeviceAddress
	// The kernel driver does not enforce page alignment on the device
	// address although it will be implicitly rounded down to a page
	// boundary. We do it explicitly because it simplifies tracking
	// of allocated ranges in 'devAddrSet'.
	devAddr &^= (hostarch.PageSize - 1)

	// Make sure that the device address range can be mapped.
	devar := DevAddrRange{
		devAddr,
		devAddr + userIoctlParams.Size,
	}
	if !devar.WellFormed() {
		return 0, linuxerr.EINVAL
	}

	// Reserve a range in our address space.
	m, _, errno := unix.RawSyscall6(unix.SYS_MMAP, 0 /* addr */, uintptr(ar.Length()), unix.PROT_NONE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS, ^uintptr(0) /* fd */, 0 /* offset */)
	if errno != 0 {
		return 0, errno
	}
	cu := cleanup.Make(func() {
		unix.RawSyscall(unix.SYS_MUNMAP, m, uintptr(ar.Length()), 0)
	})
	defer cu.Clean()
	// Mirror application mappings into the reserved range.
	prs, err := t.MemoryManager().Pin(ctx, ar, hostarch.ReadWrite, false /* ignorePermissions */)
	cu.Add(func() {
		mm.Unpin(prs)
	})
	if err != nil {
		return 0, err
	}
	sentryAddr := uintptr(m)
	for _, pr := range prs {
		ims, err := pr.File.MapInternal(memmap.FileRange{pr.Offset, pr.Offset + uint64(pr.Source.Length())}, hostarch.ReadWrite)
		if err != nil {
			return 0, err
		}
		for !ims.IsEmpty() {
			im := ims.Head()
			if _, _, errno := unix.RawSyscall6(unix.SYS_MREMAP, im.Addr(), 0 /* old_size */, uintptr(im.Len()), linux.MREMAP_MAYMOVE|linux.MREMAP_FIXED, sentryAddr, 0); errno != 0 {
				return 0, errno
			}
			sentryAddr += uintptr(im.Len())
			ims = ims.Tail()
		}
	}
	sentryIoctlParams := userIoctlParams
	sentryIoctlParams.HostAddress = uint64(m)
	n, err := util.IOCTLInvokePtrArg[gasket.Ioctl](hostFd, gasket.GASKET_IOCTL_MAP_BUFFER, &sentryIoctlParams)
	if err != nil {
		return n, err
	}
	cu.Release()
	// Unmap the reserved range, which is no longer required.
	unix.RawSyscall(unix.SYS_MUNMAP, m, uintptr(ar.Length()), 0)

	fd.device.mu.Lock()
	defer fd.device.mu.Unlock()
	for _, pr := range prs {
		rlen := uint64(pr.Source.Length())
		fd.device.devAddrSet.InsertRange(DevAddrRange{
			devAddr,
			devAddr + rlen,
		}, pinnedAccelMem{pinnedRange: pr, pageTableIndex: userIoctlParams.PageTableIndex})
		devAddr += rlen
	}
	return n, nil
}

func gasketUnmapBufferIoctl(ctx context.Context, t *kernel.Task, hostFd int32, fd *accelFD, paramsAddr hostarch.Addr) (uintptr, error) {
	var userIoctlParams gasket.GasketPageTableIoctl
	if _, err := userIoctlParams.CopyIn(t, paramsAddr); err != nil {
		return 0, err
	}

	numberOfPageTables := tpu.NumberOfTPUV4PageTables
	if fd.device.lite {
		numberOfPageTables = tpu.NumberOfTPUV4litePageTables
	}
	if userIoctlParams.PageTableIndex >= numberOfPageTables {
		return 0, linuxerr.EFAULT
	}

	devAddr := userIoctlParams.DeviceAddress
	devAddr &^= (hostarch.PageSize - 1)
	devar := DevAddrRange{
		devAddr,
		devAddr + userIoctlParams.Size,
	}
	if !devar.WellFormed() {
		return 0, linuxerr.EINVAL
	}

	sentryIoctlParams := userIoctlParams
	sentryIoctlParams.HostAddress = 0 // clobber this value, it's unused.
	n, err := util.IOCTLInvokePtrArg[gasket.Ioctl](hostFd, gasket.GASKET_IOCTL_UNMAP_BUFFER, &sentryIoctlParams)
	if err != nil {
		return n, err
	}
	fd.device.mu.Lock()
	defer fd.device.mu.Unlock()
	s := &fd.device.devAddrSet
	r := DevAddrRange{userIoctlParams.DeviceAddress, userIoctlParams.DeviceAddress + userIoctlParams.Size}
	s.RemoveRangeWith(r, func(seg DevAddrIterator) {
		mm.Unpin([]mm.PinnedRange{seg.ValuePtr().pinnedRange})
	})
	return n, nil
}

func gasketInterruptMappingIoctl(ctx context.Context, t *kernel.Task, hostFd int32, paramsAddr hostarch.Addr, lite bool) (uintptr, error) {
	var userIoctlParams gasket.GasketInterruptMapping
	if _, err := userIoctlParams.CopyIn(t, paramsAddr); err != nil {
		return 0, err
	}

	sizeOfInterruptList := tpu.SizeOfTPUV4InterruptList
	interruptMap := tpu.TPUV4InterruptsMap
	if lite {
		sizeOfInterruptList = tpu.SizeOfTPUV4liteInterruptList
		interruptMap = tpu.TPUV4liteInterruptsMap
	}
	if userIoctlParams.Interrupt >= sizeOfInterruptList {
		return 0, linuxerr.EINVAL
	}
	barRegMap, ok := interruptMap[userIoctlParams.BarIndex]
	if !ok {
		return 0, linuxerr.EINVAL
	}
	if _, ok := barRegMap[userIoctlParams.RegOffset]; !ok {
		return 0, linuxerr.EINVAL
	}

	// Check that 'userEventFD.Eventfd' is an eventfd.
	eventFileGeneric, _ := t.FDTable().Get(int32(userIoctlParams.EventFD))
	if eventFileGeneric == nil {
		return 0, linuxerr.EBADF
	}
	defer eventFileGeneric.DecRef(ctx)
	eventFile, ok := eventFileGeneric.Impl().(*eventfd.EventFileDescription)
	if !ok {
		return 0, linuxerr.EINVAL
	}

	eventfd, err := eventFile.HostFD()
	if err != nil {
		return 0, err
	}

	sentryIoctlParams := userIoctlParams
	sentryIoctlParams.EventFD = uint64(eventfd)
	n, err := util.IOCTLInvokePtrArg[gasket.Ioctl](hostFd, gasket.GASKET_IOCTL_REGISTER_INTERRUPT, &sentryIoctlParams)
	if err != nil {
		return n, err
	}

	outIoctlParams := sentryIoctlParams
	outIoctlParams.EventFD = userIoctlParams.EventFD
	if _, err := outIoctlParams.CopyOut(t, paramsAddr); err != nil {
		return n, err
	}
	return n, nil
}
