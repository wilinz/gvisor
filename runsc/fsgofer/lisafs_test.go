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

package lisafs_test

import (
	"testing"

	"github.com/wilinz/gvisor/pkg/lisafs"
	"github.com/wilinz/gvisor/pkg/lisafs/testsuite"
	"github.com/wilinz/gvisor/pkg/log"
	"github.com/wilinz/gvisor/runsc/fsgofer"
)

// Note that these are not supposed to be extensive or robust tests. These unit
// tests provide a sanity check that all RPCs at least work in obvious ways.

func init() {
	log.SetLevel(log.Debug)
	if err := fsgofer.OpenProcSelfFD("/proc/self/fd"); err != nil {
		panic(err)
	}
}

// tester implements testsuite.Tester.
type tester struct{}

// NewServer implements testsuite.Tester.NewServer.
func (tester) NewServer(t *testing.T) *lisafs.Server {
	return &fsgofer.NewLisafsServer(fsgofer.Config{}).Server
}

// LinkSupported implements testsuite.Tester.LinkSupported.
func (tester) LinkSupported() bool {
	return true
}

// SetUserGroupIDSupported implements testsuite.Tester.SetUserGroupIDSupported.
func (tester) SetUserGroupIDSupported() bool {
	return true
}

// BindSupported implements testsuite.Tester.BindSupported.
func (tester) BindSupported() bool {
	// In some test environments, the mount path is really large and bind(2)
	// fails with EINVAL if the path length >= 108.
	return false
}

func TestFSGofer(t *testing.T) {
	testsuite.RunAllLocalFSTests(t, tester{})
}
