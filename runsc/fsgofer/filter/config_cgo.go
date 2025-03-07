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

package filter

import (
	"golang.org/x/sys/unix"
	"github.com/wilinz/gvisor/pkg/seccomp"
)

var cgoFilters = seccomp.MakeSyscallRules(map[uintptr]seccomp.SyscallRule{
	unix.SYS_MMAP: seccomp.Or{
		seccomp.PerArg{
			seccomp.AnyValue{},
			seccomp.AnyValue{},
			seccomp.EqualTo(unix.PROT_NONE),
			seccomp.EqualTo(
				unix.MAP_PRIVATE |
					unix.MAP_ANONYMOUS |
					unix.MAP_NORESERVE),
		},
		seccomp.PerArg{
			seccomp.AnyValue{},
			seccomp.AnyValue{},
			seccomp.AnyValue{},
			seccomp.EqualTo(
				unix.MAP_PRIVATE |
					unix.MAP_ANONYMOUS |
					unix.MAP_STACK),
		},
	},
	// TODO(eperot): remove this syscall seccomp rule
	unix.SYS_SET_ROBUST_LIST: seccomp.MatchAll{},
	// TODO(eperot): remove this syscall seccomp rule
	unix.SYS_CLONE3: seccomp.MatchAll{},
	// TODO(eperot): remove this syscall seccomp rule
	unix.SYS_RSEQ: seccomp.MatchAll{},
})
