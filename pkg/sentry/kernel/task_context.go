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

package kernel

import (
	"time"

	"github.com/wilinz/gvisor/pkg/abi/linux"
	"github.com/wilinz/gvisor/pkg/context"
	"github.com/wilinz/gvisor/pkg/cpuid"
	"github.com/wilinz/gvisor/pkg/devutil"
	"github.com/wilinz/gvisor/pkg/sentry/inet"
	"github.com/wilinz/gvisor/pkg/sentry/kernel/auth"
	"github.com/wilinz/gvisor/pkg/sentry/kernel/ipc"
	"github.com/wilinz/gvisor/pkg/sentry/kernel/shm"
	"github.com/wilinz/gvisor/pkg/sentry/ktime"
	"github.com/wilinz/gvisor/pkg/sentry/limits"
	"github.com/wilinz/gvisor/pkg/sentry/pgalloc"
	"github.com/wilinz/gvisor/pkg/sentry/platform"
	"github.com/wilinz/gvisor/pkg/sentry/unimpl"
	"github.com/wilinz/gvisor/pkg/sentry/uniqueid"
	"github.com/wilinz/gvisor/pkg/sentry/vfs"
	"github.com/wilinz/gvisor/pkg/sync"
)

// Deadline implements context.Context.Deadline.
func (*Task) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

// Done implements context.Context.Done.
func (*Task) Done() <-chan struct{} {
	return nil
}

// Err implements context.Context.Err.
func (*Task) Err() error {
	return nil
}

// Value implements context.Context.Value.
//
// Preconditions: The caller must be running on the task goroutine.
func (t *Task) Value(key any) any {
	// This function is very hot; skip this check outside of +race builds.
	if sync.RaceEnabled {
		t.assertTaskGoroutine()
	}
	return t.contextValue(key, true /* isTaskGoroutine */)
}

func (t *Task) contextValue(key any, isTaskGoroutine bool) any {
	switch key {
	case CtxCanTrace:
		return t.CanTrace
	case CtxKernel:
		return t.k
	case CtxPIDNamespace:
		return t.tg.pidns
	case CtxUTSNamespace:
		if !isTaskGoroutine {
			t.mu.Lock()
			defer t.mu.Unlock()
		}
		utsns := t.utsns
		utsns.IncRef()
		return utsns
	case ipc.CtxIPCNamespace:
		if !isTaskGoroutine {
			t.mu.Lock()
			defer t.mu.Unlock()
		}
		ipcns := t.ipcns
		ipcns.IncRef()
		return ipcns
	case CtxTask:
		return t
	case auth.CtxCredentials:
		return t.creds.Load()
	case auth.CtxThreadGroupID:
		return int32(t.tg.ID())
	case vfs.CtxRoot:
		if !isTaskGoroutine {
			t.mu.Lock()
			defer t.mu.Unlock()
		}
		return t.fsContext.RootDirectory()
	case vfs.CtxMountNamespace:
		if !isTaskGoroutine {
			t.mu.Lock()
			defer t.mu.Unlock()
		}
		t.mountNamespace.IncRef()
		return t.mountNamespace
	case devutil.CtxDevGoferClient:
		return t.k.GetDevGoferClient(t.k.ContainerName(t.containerID))
	case inet.CtxStack:
		return t.NetworkContext()
	case inet.CtxNamespaceByFD:
		return t.NetworkNamespaceByFD
	case ktime.CtxRealtimeClock:
		return t.k.RealtimeClock()
	case limits.CtxLimits:
		return t.tg.limits
	case linux.CtxSignalNoInfoFunc:
		return func(sig linux.Signal) error {
			return t.SendSignal(SignalInfoNoInfo(sig, t, t))
		}
	case pgalloc.CtxMemoryCgroupID:
		return t.memCgID.Load()
	case pgalloc.CtxMemoryFile:
		return t.k.mf
	case platform.CtxPlatform:
		return t.k
	case shm.CtxDeviceID:
		return t.k.sysVShmDevID
	case uniqueid.CtxGlobalUniqueID:
		return t.k.UniqueID()
	case uniqueid.CtxGlobalUniqueIDProvider:
		return t.k
	case uniqueid.CtxInotifyCookie:
		return t.k.GenerateInotifyCookie()
	case unimpl.CtxEvents:
		return t.k
	case cpuid.CtxFeatureSet:
		return t.k.featureSet
	default:
		return nil
	}
}

// fallbackContext adds a level of indirection for embedding to resolve
// ambiguity for method resolution. We favor context.NoTask.
type fallbackTask struct {
	*Task
}

// taskAsyncContext implements context.Context for a goroutine that performs
// work on behalf of a Task, but is not the task goroutine.
type taskAsyncContext struct {
	context.NoTask
	fallbackTask
}

// Value implements context.Context.Value.
func (t *taskAsyncContext) Value(key any) any {
	return t.fallbackTask.contextValue(key, false /* isTaskGoroutine */)
}

// AsyncContext returns a context.Context representing t. The returned
// context.Context is intended for use by goroutines other than t's task
// goroutine; for example, signal delivery to t will not interrupt goroutines
// that are blocking using the returned context.Context.
func (t *Task) AsyncContext() context.Context {
	return &taskAsyncContext{
		fallbackTask: fallbackTask{t},
	}
}
