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

package boot

import (
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/wilinz/gvisor/runsc/config"
)

func TestGetMountAccessType(t *testing.T) {
	const source = "foo"
	for _, tst := range []struct {
		name        string
		annotations map[string]string
		want        config.FileAccessType
	}{
		{
			name: "container=exclusive",
			annotations: map[string]string{
				MountPrefix + "mount1.source": source,
				MountPrefix + "mount1.type":   "bind",
				MountPrefix + "mount1.share":  "container",
			},
			want: config.FileAccessExclusive,
		},
		{
			name: "pod=shared",
			annotations: map[string]string{
				MountPrefix + "mount1.source": source,
				MountPrefix + "mount1.type":   "bind",
				MountPrefix + "mount1.share":  "pod",
			},
			want: config.FileAccessShared,
		},
		{
			name: "share=shared",
			annotations: map[string]string{
				MountPrefix + "mount1.source": source,
				MountPrefix + "mount1.type":   "bind",
				MountPrefix + "mount1.share":  "shared",
			},
			want: config.FileAccessShared,
		},
		{
			name: "default=shared",
			annotations: map[string]string{
				MountPrefix + "mount1.source": source + "mismatch",
				MountPrefix + "mount1.type":   "bind",
				MountPrefix + "mount1.share":  "container",
			},
			want: config.FileAccessShared,
		},
		{
			name: "tmpfs+container=exclusive",
			annotations: map[string]string{
				MountPrefix + "mount1.source": source,
				MountPrefix + "mount1.type":   "tmpfs",
				MountPrefix + "mount1.share":  "container",
			},
			want: config.FileAccessExclusive,
		},
		{
			name: "tmpfs+pod=exclusive",
			annotations: map[string]string{
				MountPrefix + "mount1.source": source,
				MountPrefix + "mount1.type":   "tmpfs",
				MountPrefix + "mount1.share":  "pod",
			},
			want: config.FileAccessExclusive,
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			spec := &specs.Spec{Annotations: tst.annotations}
			podHints, err := NewPodMountHints(spec)
			if err != nil {
				t.Fatalf("newPodMountHints failed: %v", err)
			}
			conf := &config.Config{FileAccessMounts: config.FileAccessShared}
			if got := getMountAccessType(conf, podHints.FindMount(source)); got != tst.want {
				t.Errorf("getMountAccessType(), got: %v, want: %v", got, tst.want)
			}
		})
	}
}
