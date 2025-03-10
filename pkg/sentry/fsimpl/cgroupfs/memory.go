// Copyright 2021 The gVisor Authors.
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

package cgroupfs

import (
	"bytes"
	"fmt"
	"math"

	"github.com/wilinz/gvisor/pkg/abi/linux"
	"github.com/wilinz/gvisor/pkg/atomicbitops"
	"github.com/wilinz/gvisor/pkg/context"
	"github.com/wilinz/gvisor/pkg/sentry/fsimpl/kernfs"
	"github.com/wilinz/gvisor/pkg/sentry/kernel"
	"github.com/wilinz/gvisor/pkg/sentry/kernel/auth"
	"github.com/wilinz/gvisor/pkg/sentry/usage"
)

// +stateify savable
type memoryController struct {
	controllerCommon
	controllerNoResource

	limitBytes            atomicbitops.Int64
	softLimitBytes        atomicbitops.Int64
	moveChargeAtImmigrate atomicbitops.Int64
	pressureLevel         int64

	// memCg is the memory cgroup for this controller.
	memCg *memoryCgroup
}

var _ controller = (*memoryController)(nil)

func newMemoryController(fs *filesystem, defaults map[string]int64) *memoryController {
	c := &memoryController{
		// Linux sets these limits to (PAGE_COUNTER_MAX * PAGE_SIZE) by default,
		// which is ~ 2**63 on a 64-bit system. So essentially, infinity. The
		// exact value isn't very important.

		limitBytes:     atomicbitops.FromInt64(math.MaxInt64),
		softLimitBytes: atomicbitops.FromInt64(math.MaxInt64),
	}

	consumeDefault := func(name string, valPtr *atomicbitops.Int64) {
		if val, ok := defaults[name]; ok {
			valPtr.Store(val)
			delete(defaults, name)
		}
	}

	consumeDefault("memory.limit_in_bytes", &c.limitBytes)
	consumeDefault("memory.soft_limit_in_bytes", &c.softLimitBytes)
	consumeDefault("memory.move_charge_at_immigrate", &c.moveChargeAtImmigrate)

	c.controllerCommon.init(kernel.CgroupControllerMemory, fs)
	return c
}

// Clone implements controller.Clone.
func (c *memoryController) Clone() controller {
	new := &memoryController{
		limitBytes:            atomicbitops.FromInt64(c.limitBytes.Load()),
		softLimitBytes:        atomicbitops.FromInt64(c.softLimitBytes.Load()),
		moveChargeAtImmigrate: atomicbitops.FromInt64(c.moveChargeAtImmigrate.Load()),
	}
	new.controllerCommon.cloneFromParent(c)
	return new
}

// AddControlFiles implements controller.AddControlFiles.
func (c *memoryController) AddControlFiles(ctx context.Context, creds *auth.Credentials, cg *cgroupInode, contents map[string]kernfs.Inode) {
	c.memCg = &memoryCgroup{cg}
	contents["memory.usage_in_bytes"] = c.fs.newControllerFile(ctx, creds, &memoryUsageInBytesData{memCg: &memoryCgroup{cg}}, true)
	contents["memory.limit_in_bytes"] = c.fs.newStubControllerFile(ctx, creds, &c.limitBytes, true)
	contents["memory.soft_limit_in_bytes"] = c.fs.newStubControllerFile(ctx, creds, &c.softLimitBytes, true)
	contents["memory.move_charge_at_immigrate"] = c.fs.newStubControllerFile(ctx, creds, &c.moveChargeAtImmigrate, true)
	contents["memory.pressure_level"] = c.fs.newStaticControllerFile(ctx, creds, linux.FileMode(0644), fmt.Sprintf("%d\n", c.pressureLevel))
}

// Enter implements controller.Enter.
func (c *memoryController) Enter(t *kernel.Task) {
	// Update the new cgroup id for the task.
	t.SetMemCgID(c.memCg.ID())
}

// Leave implements controller.Leave.
func (c *memoryController) Leave(t *kernel.Task) {
	// Update the cgroup id for the task to zero.
	t.SetMemCgID(0)
}

// PrepareMigrate implements controller.PrepareMigrate.
func (c *memoryController) PrepareMigrate(t *kernel.Task, src controller) error {
	return nil
}

// CommitMigrate implements controller.CommitMigrate.
func (c *memoryController) CommitMigrate(t *kernel.Task, src controller) {
	// Start tracking t at dst by updating the memCgID.
	t.SetMemCgID(c.memCg.ID())
}

// AbortMigrate implements controller.AbortMigrate.
func (c *memoryController) AbortMigrate(t *kernel.Task, src controller) {}

// +stateify savable
type memoryCgroup struct {
	*cgroupInode
}

// Collects all the memory cgroup ids for the cgroup.
func (memCg *memoryCgroup) collectMemCgIDs(memCgIDs map[uint32]struct{}) {
	// Add ourselves.
	memCgIDs[memCg.ID()] = struct{}{}
	// Add our children.
	memCg.forEachChildDir(func(d *dir) {
		cg := memoryCgroup{d.cgi}
		cg.collectMemCgIDs(memCgIDs)
	})
}

// Returns the memory usage for all cgroup ids in memCgIDs.
func getUsage(k *kernel.Kernel, memCgIDs map[uint32]struct{}) uint64 {
	k.MemoryFile().UpdateUsage(memCgIDs)
	var totalBytes uint64
	for id := range memCgIDs {
		_, bytes := usage.MemoryAccounting.CopyPerCg(id)
		totalBytes += bytes
	}
	return totalBytes
}

// +stateify savable
type memoryUsageInBytesData struct {
	memCg *memoryCgroup
}

// Generate implements vfs.DynamicBytesSource.Generate.
func (d *memoryUsageInBytesData) Generate(ctx context.Context, buf *bytes.Buffer) error {
	k := kernel.KernelFromContext(ctx)

	memCgIDs := make(map[uint32]struct{})
	d.memCg.collectMemCgIDs(memCgIDs)
	totalBytes := getUsage(k, memCgIDs)
	fmt.Fprintf(buf, "%d\n", totalBytes)
	return nil
}
