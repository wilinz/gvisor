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

// Package filter_bench_test benchmarks the speed of the seccomp-bpf filters.
package filter_bench_test

import (
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
	"github.com/wilinz/gvisor/pkg/abi/linux"
	"github.com/wilinz/gvisor/pkg/seccomp"
	"github.com/wilinz/gvisor/pkg/sentry/devices/nvproxy/nvconf"
	"github.com/wilinz/gvisor/pkg/sentry/platform/kvm"
	"github.com/wilinz/gvisor/pkg/sentry/platform/systrap"
	"github.com/wilinz/gvisor/runsc/boot/filter/config"
	"github.com/wilinz/gvisor/test/secbench"
	"github.com/wilinz/gvisor/test/secbench/secbenchdef"
)

type Options struct {
	Name    string
	Options config.Options
}

// BenchmarkSentrySystrap benchmarks the seccomp filters used by the Sentry
// using the Systrap platform.
func BenchmarkSentrySystrap(b *testing.B) {
	opts := config.Options{
		Platform: (&systrap.Systrap{}).SeccompInfo(),
	}
	rules, denyRules := config.Rules(opts)
	secbench.Run(b, secbench.BenchFromSyscallRules(
		b,
		"Postgres",
		secbenchdef.Profile{
			Arch: linux.AUDIT_ARCH_X86_64,
			Sequences: []secbenchdef.Sequence{
				// Top 10 syscalls captured by running Postgres in a runsc container
				// and running `pgbench` against it. Weights are the number of times
				// each syscall was called.
				{"futex", 870063, secbenchdef.Single(unix.SYS_FUTEX, 0, linux.FUTEX_WAKE)},
				{"nanosleep", 275649, secbenchdef.NanosleepZero.Seq()},
				{"sendmmsg", 160201, secbenchdef.Single(unix.SYS_SENDMMSG, secbenchdef.NonExistentFD, 0, 0, unix.MSG_DONTWAIT)},
				{"fstat", 115769, secbenchdef.Single(unix.SYS_FSTAT, secbenchdef.NonExistentFD)},
				{"ppoll", 69749, secbenchdef.PPollNonExistent.Seq()},
				{"fsync", 23131, secbenchdef.Single(unix.SYS_FSYNC, secbenchdef.NonExistentFD)},
				{"pwrite64", 14096, secbenchdef.Single(unix.SYS_PWRITE64, secbenchdef.NonExistentFD)},
				{"epoll_pwait", 12266, secbenchdef.Single(unix.SYS_EPOLL_PWAIT, secbenchdef.NonExistentFD)},
				{"close", 1991, secbenchdef.Single(unix.SYS_CLOSE, secbenchdef.NonExistentFD)},
				{"getpid", 1413, secbenchdef.Single(unix.SYS_GETPID)},
			},
		},
		rules,
		denyRules,
		config.SeccompOptions(opts),
	))
}

// BenchmarkSentryKVM benchmarks the seccomp filters used by the Sentry
// using the KVM platform.
func BenchmarkSentryKVM(b *testing.B) {
	opts := config.Options{
		Platform: (&kvm.KVM{}).SeccompInfo(),
	}
	rules, denyRules := config.Rules(opts)
	secbench.Run(b, secbench.BenchFromSyscallRules(
		b,
		"Postgres",
		secbenchdef.Profile{
			Arch: linux.AUDIT_ARCH_X86_64,
			Sequences: []secbenchdef.Sequence{
				// Same procedure, but using the KVM platform instead.
				{"futex", 3180352, secbenchdef.Single(unix.SYS_FUTEX, 0, linux.FUTEX_WAKE)},
				{"ioctl", 2501786, secbenchdef.Single(unix.SYS_IOCTL, secbenchdef.NonExistentFD, kvm.KVM_RUN)},
				{"rt_sigreturn", 2501695, secbenchdef.RTSigreturn.Seq()},
				{"sendmmsg", 1490395, secbenchdef.Single(unix.SYS_SENDMMSG, secbenchdef.NonExistentFD, 0, 0, unix.MSG_DONTWAIT)},
				{"nanosleep", 1217019, secbenchdef.NanosleepZero.Seq()},
				{"fstat", 1068477, secbenchdef.Single(unix.SYS_FSTAT, secbenchdef.NonExistentFD)},
				{"ppoll", 653137, secbenchdef.PPollNonExistent.Seq()},
				{"fsync", 213320, secbenchdef.Single(unix.SYS_FSYNC, secbenchdef.NonExistentFD)},
				{"pwrite64", 107603, secbenchdef.Single(unix.SYS_PWRITE64, secbenchdef.NonExistentFD)},
				{"epoll_pwait", 29909, secbenchdef.Single(unix.SYS_EPOLL_PWAIT, secbenchdef.NonExistentFD)},
			},
		},
		rules,
		denyRules,
		config.SeccompOptions(opts),
	))
}

func BenchmarkNVProxyIoctl(b *testing.B) {
	opts := config.Options{
		Platform:    (&systrap.Systrap{}).SeccompInfo(),
		NVProxy:     true,
		NVProxyCaps: nvconf.ValidCapabilities,
	}
	rules, denyRules := config.Rules(opts)
	var sequences []secbenchdef.Sequence
	if err := rules.ForSingleArgument(unix.SYS_IOCTL, 1, func(v seccomp.ValueMatcher) error {
		if arg1Equal, isArg1Equal := v.(seccomp.EqualTo); isArg1Equal {
			sequences = append(sequences, secbenchdef.Sequence{
				Name:     fmt.Sprintf("ioctl_%d", arg1Equal),
				Weight:   1,
				Syscalls: secbenchdef.Single(unix.SYS_IOCTL, 0, uintptr(arg1Equal)),
			})
		}
		return nil
	}); err != nil {
		b.Fatalf("ioctl rules are not well-formed: %v", err)
	}
	secbench.Run(b, secbench.BenchFromSyscallRules(
		b,
		"nvproxy",
		secbenchdef.Profile{
			Arch:      linux.AUDIT_ARCH_X86_64,
			Sequences: sequences,
		},
		rules,
		denyRules,
		config.SeccompOptions(opts),
	))
}
