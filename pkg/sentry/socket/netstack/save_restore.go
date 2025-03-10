// Copyright 2018 The gVisor Authors.
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

package netstack

import (
	"context"

	"github.com/wilinz/gvisor/pkg/tcpip/stack"
)

// afterLoad is invoked by stateify.
func (s *Stack) afterLoad(ctx context.Context) {
	if s.IsSaveRestoreEnabled() {
		// This indicates that netstack s/r is enabled and the stack
		// should not be replaced with the new stack from context.
		return
	}
	s.Stack = stack.RestoreStackFromContext(ctx)
	if s.Stack == nil {
		panic("can't restore without netstack/tcpip/stack.Stack")
	}
}
