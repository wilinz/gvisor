// Copyright 2023 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package accel implements a proxy for gasket based accel devices.
package accel

import (
	"fmt"

	"golang.org/x/sys/unix"
	"github.com/wilinz/gvisor/pkg/abi/linux"
	"github.com/wilinz/gvisor/pkg/context"
	"github.com/wilinz/gvisor/pkg/devutil"
	"github.com/wilinz/gvisor/pkg/errors/linuxerr"
	"github.com/wilinz/gvisor/pkg/fdnotifier"
	"github.com/wilinz/gvisor/pkg/log"
	"github.com/wilinz/gvisor/pkg/sentry/kernel"
	"github.com/wilinz/gvisor/pkg/sentry/vfs"
	"github.com/wilinz/gvisor/pkg/sync"
)

// accelDevice implements vfs.Device for /dev/accel[0-9]+.
//
// +stateify savable
type accelDevice struct {
	mu sync.Mutex

	minor uint32
	lite  bool
	// +checklocks:mu
	openWriteFDs uint32
	// +checklocks:mu
	devAddrSet DevAddrSet
	// +checklocks:mu
	owner *kernel.ThreadGroup
}

func (dev *accelDevice) Open(ctx context.Context, mnt *vfs.Mount, vfsd *vfs.Dentry, opts vfs.OpenOptions) (*vfs.FileDescription, error) {
	devClient := devutil.GoferClientFromContext(ctx)
	if devClient == nil {
		log.Warningf("devutil.CtxDevGoferClient is not set")
		return nil, linuxerr.ENOENT
	}
	dev.mu.Lock()
	defer dev.mu.Unlock()
	name := fmt.Sprintf("accel%d", dev.minor)
	hostFD, err := devClient.OpenAt(ctx, name, opts.Flags)
	if err != nil {
		ctx.Warningf("accelDevice: failed to open device %s: %v", name, err)
		return nil, err
	}
	fd := &accelFD{
		hostFD: int32(hostFD),
		device: dev,
	}
	if err := fd.vfsfd.Init(fd, opts.Flags, mnt, vfsd, &vfs.FileDescriptionOptions{
		UseDentryMetadata: true,
	}); err != nil {
		unix.Close(hostFD)
		return nil, err
	}
	if err := fdnotifier.AddFD(int32(hostFD), &fd.queue); err != nil {
		unix.Close(hostFD)
		return nil, err
	}
	fd.memmapFile.fd = fd
	if vfs.MayWriteFileWithOpenFlags(opts.Flags) {
		dev.openWriteFDs++
	}
	if dev.owner == nil {
		t := kernel.TaskFromContext(ctx)
		if t == nil {
			return nil, linuxerr.ESRCH
		}
		dev.owner = t.ThreadGroup()
	}
	return &fd.vfsfd, nil
}

// RegisterTPUDevice registers all devices implemented by this package in vfsObj.
func RegisterTPUDevice(vfsObj *vfs.VirtualFilesystem, minor uint32, lite bool) error {
	if vfsObj.IsDeviceRegistered(vfs.CharDevice, linux.ACCEL_MAJOR, minor) {
		return nil
	}
	return vfsObj.RegisterDevice(vfs.CharDevice, linux.ACCEL_MAJOR, minor, &accelDevice{
		lite:  lite,
		minor: minor,
	}, &vfs.RegisterDeviceOptions{
		GroupName: "accel",
	})
}
