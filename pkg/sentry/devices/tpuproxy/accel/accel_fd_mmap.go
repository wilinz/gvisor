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
	"github.com/wilinz/gvisor/pkg/context"
	"github.com/wilinz/gvisor/pkg/errors/linuxerr"
	"github.com/wilinz/gvisor/pkg/hostarch"
	"github.com/wilinz/gvisor/pkg/log"
	"github.com/wilinz/gvisor/pkg/safemem"
	"github.com/wilinz/gvisor/pkg/sentry/memmap"
	"github.com/wilinz/gvisor/pkg/sentry/vfs"
)

// ConfigureMMap implements vfs.FileDescriptionImpl.ConfigureMMap.
func (fd *accelFD) ConfigureMMap(ctx context.Context, opts *memmap.MMapOpts) error {
	return vfs.GenericProxyDeviceConfigureMMap(&fd.vfsfd, fd, opts)
}

// AddMapping implements memmap.Mappable.AddMapping.
func (fd *accelFD) AddMapping(ctx context.Context, ms memmap.MappingSpace, ar hostarch.AddrRange, offset uint64, writable bool) error {
	return nil
}

// RemoveMapping implements memmap.Mappable.RemoveMapping.
func (fd *accelFD) RemoveMapping(ctx context.Context, ms memmap.MappingSpace, ar hostarch.AddrRange, offset uint64, writable bool) {
}

// CopyMapping implements memmap.Mappable.CopyMapping.
func (fd *accelFD) CopyMapping(ctx context.Context, ms memmap.MappingSpace, srcAR, dstAR hostarch.AddrRange, offset uint64, writable bool) error {
	return nil
}

// Translate implements memmap.Mappable.Translate.
func (fd *accelFD) Translate(ctx context.Context, required, optional memmap.MappableRange, at hostarch.AccessType) ([]memmap.Translation, error) {
	return []memmap.Translation{
		{
			Source: optional,
			File:   &fd.memmapFile,
			Offset: optional.Start,
			Perms:  hostarch.AnyAccess,
		},
	}, nil
}

// InvalidateUnsavable implements memmap.Mappable.InvalidateUnsavable.
func (fd *accelFD) InvalidateUnsavable(ctx context.Context) error {
	return nil
}

type accelFDMemmapFile struct {
	memmap.NoBufferedIOFallback

	fd *accelFD
}

// IncRef implements memmap.File.IncRef.
func (mf *accelFDMemmapFile) IncRef(memmap.FileRange, uint32) {
}

// DecRef implements memmap.File.DecRef.
func (mf *accelFDMemmapFile) DecRef(fr memmap.FileRange) {
}

// MapInternal implements memmap.File.MapInternal.
func (mf *accelFDMemmapFile) MapInternal(fr memmap.FileRange, at hostarch.AccessType) (safemem.BlockSeq, error) {
	log.Traceback("accel: rejecting accelFDMemmapFile.MapInternal")
	return safemem.BlockSeq{}, linuxerr.EINVAL
}

// DataFD implements memmap.File.DataFD.
func (mf *accelFDMemmapFile) DataFD(fr memmap.FileRange) (int, error) {
	return mf.FD(), nil
}

// FD implements memmap.File.FD.
func (mf *accelFDMemmapFile) FD() int {
	return int(mf.fd.hostFD)
}
