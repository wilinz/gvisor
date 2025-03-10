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

package overlay

import (
	"context"

	"github.com/wilinz/gvisor/pkg/refs"
)

func (d *dentry) afterLoad(context.Context) {
	if d.refs.Load() != -1 {
		refs.Register(d)
	}
}

// saveParent is called by stateify.
func (d *dentry) saveParent() *dentry {
	return d.parent.Load()
}

// loadParent is called by stateify.
func (d *dentry) loadParent(_ context.Context, parent *dentry) {
	d.parent.Store(parent)
}
