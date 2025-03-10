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

package container

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wilinz/gvisor/pkg/sentry/control"
	"github.com/wilinz/gvisor/pkg/sentry/kernel/auth"
	"github.com/wilinz/gvisor/pkg/test/testutil"
	"github.com/wilinz/gvisor/runsc/config"
)

// TestSharedVolume checks that modifications to a volume mount are propagated
// into and out of the sandbox.
func TestSharedVolume(t *testing.T) {
	conf := testutil.TestConfig(t)
	conf.Overlay2.Set("none")
	conf.FileAccess = config.FileAccessShared

	// Main process just sleeps. We will use "exec" to probe the state of
	// the filesystem.
	spec := testutil.NewSpecWithArgs("sleep", "1000")

	dir, err := os.MkdirTemp(testutil.TmpDir(), "shared-volume-test")
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}

	_, bundleDir, cleanup, err := testutil.SetupContainer(spec, conf)
	if err != nil {
		t.Fatalf("error setting up container: %v", err)
	}
	defer cleanup()

	// Create and start the container.
	args := Args{
		ID:        testutil.RandomContainerID(),
		Spec:      spec,
		BundleDir: bundleDir,
	}
	c, err := New(conf, args)
	if err != nil {
		t.Fatalf("error creating container: %v", err)
	}
	defer c.Destroy()
	if err := c.Start(conf); err != nil {
		t.Fatalf("error starting container: %v", err)
	}

	// File that will be used to check consistency inside/outside sandbox.
	filename := filepath.Join(dir, "file")

	// File does not exist yet. Reading from the sandbox should fail.
	argsTestFile := &control.ExecArgs{
		Filename: "/usr/bin/test",
		Argv:     []string{"test", "-f", filename},
	}
	if ws, err := c.executeSync(conf, argsTestFile); err != nil {
		t.Fatalf("unexpected error testing file %q: %v", filename, err)
	} else if ws.ExitStatus() == 0 {
		t.Errorf("test %q exited with code %v, wanted not zero", ws.ExitStatus(), err)
	}

	// Create the file from outside of the sandbox.
	if err := os.WriteFile(filename, []byte("foobar"), 0777); err != nil {
		t.Fatalf("error writing to file %q: %v", filename, err)
	}

	// Now we should be able to test the file from within the sandbox.
	if ws, err := c.executeSync(conf, argsTestFile); err != nil {
		t.Fatalf("unexpected error testing file %q: %v", filename, err)
	} else if ws.ExitStatus() != 0 {
		t.Errorf("test %q exited with code %v, wanted zero", filename, ws.ExitStatus())
	}

	// Rename the file from outside of the sandbox.
	newFilename := filepath.Join(dir, "newfile")
	if err := os.Rename(filename, newFilename); err != nil {
		t.Fatalf("os.Rename(%q, %q) failed: %v", filename, newFilename, err)
	}

	// File should no longer exist at the old path within the sandbox.
	if ws, err := c.executeSync(conf, argsTestFile); err != nil {
		t.Fatalf("unexpected error testing file %q: %v", filename, err)
	} else if ws.ExitStatus() == 0 {
		t.Errorf("test %q exited with code %v, wanted not zero", filename, ws.ExitStatus())
	}

	// We should be able to test the new filename from within the sandbox.
	argsTestNewFile := &control.ExecArgs{
		Filename: "/usr/bin/test",
		Argv:     []string{"test", "-f", newFilename},
	}
	if ws, err := c.executeSync(conf, argsTestNewFile); err != nil {
		t.Fatalf("unexpected error testing file %q: %v", newFilename, err)
	} else if ws.ExitStatus() != 0 {
		t.Errorf("test %q exited with code %v, wanted zero", newFilename, ws.ExitStatus())
	}

	// Delete the renamed file from outside of the sandbox.
	if err := os.Remove(newFilename); err != nil {
		t.Fatalf("error removing file %q: %v", filename, err)
	}

	// Renamed file should no longer exist at the old path within the sandbox.
	if ws, err := c.executeSync(conf, argsTestNewFile); err != nil {
		t.Fatalf("unexpected error testing file %q: %v", newFilename, err)
	} else if ws.ExitStatus() == 0 {
		t.Errorf("test %q exited with code %v, wanted not zero", newFilename, ws.ExitStatus())
	}

	// Now create the file from WITHIN the sandbox.
	argsTouch := &control.ExecArgs{
		Filename: "/usr/bin/touch",
		Argv:     []string{"touch", filename},
		KUID:     auth.KUID(os.Getuid()),
		KGID:     auth.KGID(os.Getgid()),
	}
	if ws, err := c.executeSync(conf, argsTouch); err != nil {
		t.Fatalf("unexpected error touching file %q: %v", filename, err)
	} else if ws.ExitStatus() != 0 {
		t.Errorf("touch %q exited with code %v, wanted zero", filename, ws.ExitStatus())
	}

	// File should exist outside the sandbox.
	if _, err := os.Stat(filename); err != nil {
		t.Errorf("stat %q got error %v, wanted nil", filename, err)
	}

	// Delete the file from within the sandbox.
	argsRemove := &control.ExecArgs{
		Filename: "/bin/rm",
		Argv:     []string{"rm", filename},
	}
	if ws, err := c.executeSync(conf, argsRemove); err != nil {
		t.Fatalf("unexpected error removing file %q: %v", filename, err)
	} else if ws.ExitStatus() != 0 {
		t.Errorf("remove %q exited with code %v, wanted zero", filename, ws.ExitStatus())
	}

	// File should not exist outside the sandbox.
	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Errorf("stat %q got error %v, wanted ErrNotExist", filename, err)
	}
}

func checkFile(conf *config.Config, c *Container, filename string, want []byte) error {
	cpy := filename + ".copy"
	if _, err := execute(conf, c, "/bin/cp", "-f", filename, cpy); err != nil {
		return fmt.Errorf("unexpected error copying file %q to %q: %v", filename, cpy, err)
	}
	got, err := os.ReadFile(cpy)
	if err != nil {
		return fmt.Errorf("error reading file %q: %v", filename, err)
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("file content inside the sandbox is wrong, got: %q, want: %q", got, want)
	}
	return nil
}

// TestSharedVolumeFile tests that changes to file content outside the sandbox
// is reflected inside.
func TestSharedVolumeFile(t *testing.T) {
	conf := testutil.TestConfig(t)
	conf.Overlay2.Set("none")
	conf.FileAccess = config.FileAccessShared

	// Main process just sleeps. We will use "exec" to probe the state of
	// the filesystem.
	spec := testutil.NewSpecWithArgs("sleep", "1000")

	dir, err := os.MkdirTemp(testutil.TmpDir(), "shared-volume-test")
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}

	_, bundleDir, cleanup, err := testutil.SetupContainer(spec, conf)
	if err != nil {
		t.Fatalf("error setting up container: %v", err)
	}
	defer cleanup()

	// Create and start the container.
	args := Args{
		ID:        testutil.RandomContainerID(),
		Spec:      spec,
		BundleDir: bundleDir,
	}
	c, err := New(conf, args)
	if err != nil {
		t.Fatalf("error creating container: %v", err)
	}
	defer c.Destroy()
	if err := c.Start(conf); err != nil {
		t.Fatalf("error starting container: %v", err)
	}

	// File that will be used to check consistency inside/outside sandbox.
	filename := filepath.Join(dir, "file")

	// Write file from outside the container and check that the same content is
	// read inside.
	want := []byte("host-")
	if err := os.WriteFile(filename, []byte(want), 0666); err != nil {
		t.Fatalf("Error writing to %q: %v", filename, err)
	}
	if err := checkFile(conf, c, filename, want); err != nil {
		t.Fatal(err.Error())
	}

	// Append to file inside the container and check that content is not lost.
	if _, err := execute(conf, c, "/bin/bash", "-c", "echo -n sandbox- >> "+filename); err != nil {
		t.Fatalf("unexpected error appending file %q: %v", filename, err)
	}
	want = []byte("host-sandbox-")
	if err := checkFile(conf, c, filename, want); err != nil {
		t.Fatal(err.Error())
	}

	// Write again from outside the container and check that the same content is
	// read inside.
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("Error opening file %q: %v", filename, err)
	}
	defer f.Close()
	if _, err := f.Write([]byte("host")); err != nil {
		t.Fatalf("Error writing to file %q: %v", filename, err)
	}
	want = []byte("host-sandbox-host")
	if err := checkFile(conf, c, filename, want); err != nil {
		t.Fatal(err.Error())
	}

	// Shrink file outside and check that the same content is read inside.
	if err := f.Truncate(5); err != nil {
		t.Fatalf("Error truncating file %q: %v", filename, err)
	}
	want = want[:5]
	if err := checkFile(conf, c, filename, want); err != nil {
		t.Fatal(err.Error())
	}
}

// TestSharedVolumeOverlay tests that changes to a shared volume that is
// wrapped in an overlay are not visible externally.
func TestSharedVolumeOverlay(t *testing.T) {
	conf := testutil.TestConfig(t)
	conf.Overlay2.Set("all:dir=/tmp")

	// File that will be used to check consistency inside/outside sandbox.
	// Note that TmpDir() is set up as a shared volume by NewSpecWithArgs(). So
	// changes inside TmpDir() should not be visible to the host.
	filename := filepath.Join(testutil.TmpDir(), "file")

	// Create a file in TmpDir() inside the container.
	spec := testutil.NewSpecWithArgs("/bin/bash", "-c", "echo Hello > "+filename+"; test -f "+filename)
	_, bundleDir, cleanup, err := testutil.SetupContainer(spec, conf)
	if err != nil {
		t.Fatalf("error setting up container: %v", err)
	}
	defer cleanup()

	// Create and start the container.
	args := Args{
		ID:        testutil.RandomContainerID(),
		Spec:      spec,
		BundleDir: bundleDir,
	}
	c, err := New(conf, args)
	if err != nil {
		t.Fatalf("error creating container: %v", err)
	}
	defer c.Destroy()
	if err := c.Start(conf); err != nil {
		t.Fatalf("error starting container: %v", err)
	}

	if ws, err := c.Wait(); err != nil {
		t.Errorf("failed to wait for container: %v", err)
	} else if es := ws.ExitStatus(); es != 0 {
		t.Errorf("subcontainer exited with non-zero status %d", es)
	}

	// Ensure that the file does not exist on the host.
	if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file exists on host, stat %q got error %v, wanted ErrNotExist", filename, err)
	}
}
