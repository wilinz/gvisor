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

package testutil

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/wilinz/gvisor/pkg/abi/linux"
	"github.com/wilinz/gvisor/pkg/context"
	"github.com/wilinz/gvisor/pkg/cpuid"
	"github.com/wilinz/gvisor/pkg/fspath"
	"github.com/wilinz/gvisor/pkg/memutil"
	"github.com/wilinz/gvisor/pkg/sentry/fsimpl/tmpfs"
	"github.com/wilinz/gvisor/pkg/sentry/kernel"
	"github.com/wilinz/gvisor/pkg/sentry/kernel/auth"
	"github.com/wilinz/gvisor/pkg/sentry/kernel/sched"
	"github.com/wilinz/gvisor/pkg/sentry/limits"
	"github.com/wilinz/gvisor/pkg/sentry/loader"
	"github.com/wilinz/gvisor/pkg/sentry/mm"
	"github.com/wilinz/gvisor/pkg/sentry/pgalloc"
	"github.com/wilinz/gvisor/pkg/sentry/platform"
	"github.com/wilinz/gvisor/pkg/sentry/seccheck"
	"github.com/wilinz/gvisor/pkg/sentry/socket/unix/transport"
	"github.com/wilinz/gvisor/pkg/sentry/time"
	"github.com/wilinz/gvisor/pkg/sentry/usage"
	"github.com/wilinz/gvisor/pkg/sentry/vfs"

	// Platforms are pluggable.
	_ "github.com/wilinz/gvisor/pkg/sentry/platform/kvm"
	_ "github.com/wilinz/gvisor/pkg/sentry/platform/ptrace"
)

var (
	platformFlag           = flag.String("platform", "ptrace", "specify which platform to use")
	platformDevicePathFlag = flag.String("platform_device_path", "", "path to a platform-specific device file (e.g. /dev/kvm for KVM platform). If unset, will use a sane platform-specific default.")
)

// Boot initializes a new bare bones kernel for test.
func Boot() (*kernel.Kernel, error) {
	cpuid.Initialize()
	seccheck.Initialize()

	if err := usage.Init(); err != nil {
		return nil, fmt.Errorf("setting up memory accounting: %v", err)
	}

	platformCtr, err := platform.Lookup(*platformFlag)
	if err != nil {
		return nil, fmt.Errorf("platform not found: %v", err)
	}
	deviceFile, err := platformCtr.OpenDevice(*platformDevicePathFlag)
	if err != nil {
		return nil, fmt.Errorf("creating platform: %v", err)
	}
	plat, err := platformCtr.New(deviceFile)
	if err != nil {
		return nil, fmt.Errorf("creating platform: %v", err)
	}

	k := &kernel.Kernel{
		Platform: plat,
	}

	mf, err := createMemoryFile()
	if err != nil {
		return nil, err
	}
	k.SetMemoryFile(mf)

	// Pass k as the platform since it is savable, unlike the actual platform.
	vdso, err := loader.PrepareVDSO(k.MemoryFile())
	if err != nil {
		return nil, fmt.Errorf("creating vdso: %v", err)
	}

	// Create timekeeper.
	tk := kernel.NewTimekeeper()
	params := kernel.NewVDSOParamPage(k.MemoryFile(), vdso.ParamPage.FileRange())
	tk.SetClocks(time.NewCalibratedClocks(), params)

	creds := auth.NewRootCredentials(auth.NewRootUserNamespace())

	// Initiate the Kernel object, which is required by the Context passed
	// to createVFS in order to mount (among other things) procfs.
	if err = k.Init(kernel.InitKernelArgs{
		ApplicationCores:  uint(runtime.GOMAXPROCS(-1)),
		FeatureSet:        cpuid.HostFeatureSet(),
		Timekeeper:        tk,
		RootUserNamespace: creds.UserNamespace,
		Vdso:              vdso,
		VdsoParams:        params,
		RootUTSNamespace:  kernel.NewUTSNamespace("hostname", "domain", creds.UserNamespace),
		RootIPCNamespace:  kernel.NewIPCNamespace(creds.UserNamespace),
		PIDNamespace:      kernel.NewRootPIDNamespace(creds.UserNamespace),
		UnixSocketOpts:    transport.UnixSocketOpts{},
	}); err != nil {
		return nil, fmt.Errorf("initializing kernel: %v", err)
	}

	k.VFS().MustRegisterFilesystemType(tmpfs.Name, &tmpfs.FilesystemType{}, &vfs.RegisterFilesystemTypeOptions{
		AllowUserMount: true,
		AllowUserList:  true,
	})

	ls, err := limits.NewLinuxLimitSet()
	if err != nil {
		return nil, err
	}
	tg := k.NewThreadGroup(k.RootPIDNamespace(), kernel.NewSignalHandlers(), linux.SIGCHLD, ls)
	k.TestOnlySetGlobalInit(tg)

	return k, nil
}

// CreateTask creates a new bare bones task for tests.
func CreateTask(ctx context.Context, name string, tc *kernel.ThreadGroup, mntns *vfs.MountNamespace, root, cwd vfs.VirtualDentry) (*kernel.Task, error) {
	k := kernel.KernelFromContext(ctx)
	if k == nil {
		return nil, fmt.Errorf("cannot find kernel from context")
	}

	exe, err := newFakeExecutable(ctx, k.VFS(), auth.CredentialsFromContext(ctx), root)
	if err != nil {
		return nil, err
	}
	m := mm.NewMemoryManager(k, k.MemoryFile(), k.SleepForAddressSpaceActivation)
	m.SetExecutable(ctx, exe)

	creds := auth.CredentialsFromContext(ctx)
	config := &kernel.TaskConfig{
		Kernel:           k,
		ThreadGroup:      tc,
		TaskImage:        &kernel.TaskImage{Name: name, MemoryManager: m},
		Credentials:      auth.CredentialsFromContext(ctx),
		NetworkNamespace: k.RootNetworkNamespace(),
		AllowedCPUMask:   sched.NewFullCPUSet(k.ApplicationCores()),
		UTSNamespace:     kernel.UTSNamespaceFromContext(ctx),
		IPCNamespace:     kernel.IPCNamespaceFromContext(ctx),
		MountNamespace:   mntns,
		FSContext:        kernel.NewFSContext(root, cwd, 0022),
		FDTable:          k.NewFDTable(),
		UserCounters:     k.GetUserCounters(creds.RealKUID),
	}
	config.NetworkNamespace.IncRef()
	t, err := k.TaskSet().NewTask(ctx, config)
	if err != nil {
		config.ThreadGroup.Release(ctx)
		return nil, err
	}
	return t, nil
}

func newFakeExecutable(ctx context.Context, vfsObj *vfs.VirtualFilesystem, creds *auth.Credentials, root vfs.VirtualDentry) (*vfs.FileDescription, error) {
	const name = "executable"
	pop := &vfs.PathOperation{
		Root:  root,
		Start: root,
		Path:  fspath.Parse(name),
	}
	opts := &vfs.OpenOptions{
		Flags: linux.O_RDONLY | linux.O_CREAT,
		Mode:  0777,
	}
	return vfsObj.OpenAt(ctx, creds, pop, opts)
}

func createMemoryFile() (*pgalloc.MemoryFile, error) {
	const memfileName = "test-memory"
	memfd, err := memutil.CreateMemFD(memfileName, 0)
	if err != nil {
		return nil, fmt.Errorf("error creating memfd: %v", err)
	}
	memfile := os.NewFile(uintptr(memfd), memfileName)
	mf, err := pgalloc.NewMemoryFile(memfile, pgalloc.MemoryFileOpts{})
	if err != nil {
		_ = memfile.Close()
		return nil, fmt.Errorf("error creating pgalloc.MemoryFile: %v", err)
	}
	return mf, nil
}
