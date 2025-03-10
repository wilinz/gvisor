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

package filter

import (
	"golang.org/x/sys/unix"
	"github.com/wilinz/gvisor/pkg/seccomp"
)

var profileFilters = seccomp.MakeSyscallRules(map[uintptr]seccomp.SyscallRule{
	unix.SYS_OPENAT: seccomp.PerArg{
		seccomp.AnyValue{},
		seccomp.AnyValue{},
		seccomp.EqualTo(unix.O_RDONLY | unix.O_LARGEFILE | unix.O_CLOEXEC),
	},
	unix.SYS_SETITIMER: seccomp.MatchAll{},
	unix.SYS_TIMER_CREATE: seccomp.PerArg{
		seccomp.EqualTo(unix.CLOCK_THREAD_CPUTIME_ID), /* which */
		seccomp.AnyValue{},                            /* sevp */
		seccomp.AnyValue{},                            /* timerid */
	},
	unix.SYS_TIMER_DELETE: seccomp.MatchAll{},
	unix.SYS_TIMER_SETTIME: seccomp.PerArg{
		seccomp.AnyValue{}, /* timerid */
		seccomp.EqualTo(0), /* flags */
		seccomp.AnyValue{}, /* new_value */
		seccomp.EqualTo(0), /* old_value */
	},
})
