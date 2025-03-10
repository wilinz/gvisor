// Copyright 2022 The gVisor Authors.
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

// Binary main starts a fuse server that forwards filesystem operations from
// /tmp to /fuse.
package main

import (
	"flag"
	golog "log"
	"os"
	"os/exec"
	"strings"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"golang.org/x/sys/unix"
	"github.com/wilinz/gvisor/pkg/log"
	"github.com/wilinz/gvisor/runsc/specutils"
)

var (
	dir   = flag.String("dir", "/tmp", "The directory to mount the fuse filesystem on.")
	cmd   = flag.String("cmd", "", "Command to execute after starting the fuse server. If empty, just wait after starting.")
	debug = flag.Bool("debug", true, "Whether to log FUSE traffic. Set to false for benchmarks.")
)

func waitOnMount(s *fuse.Server) {
	if _, _, err := specutils.RetryEintr(func() (uintptr, uintptr, error) {
		if err := s.WaitMount(); err != nil {
			return 0, 0, err
		}
		return 0, 0, nil
	}); err != nil {
		// We don't shutdown the serve loop. If the mount does
		// not succeed, the loop won't work and exit.
		log.Warningf(`Could not mount fuse submount "/tmp": %v`, err)
		os.Exit(1)
	}
}

func main() {
	flag.Parse()
	loopbackRoot, err := fs.NewLoopbackRoot("/fuse")
	if err != nil {
		log.Warningf("could not create loopback root: %v", err)
		os.Exit(1)
	}
	opts := &fuse.MountOptions{
		DirectMountStrict: true,
		Debug:             *debug,
		AllowOther:        true,
		// SingleThreaded adds locking the fuse server handler. We need to
		// enable this so that the go race detector doesn't detect a data race, even
		// if there isn't a logical race.
		SingleThreaded: true,
		Options:        []string{"default_permissions"},
	}
	rawFS := fs.NewNodeFS(loopbackRoot, &fs.Options{NullPermissions: true, Logger: golog.Default()})
	server, err := fuse.NewServer(rawFS, *dir, opts)
	if err != nil {
		log.Warningf("could not create fuse server: %v", err)
		os.Exit(1)
	}
	// Clear umask so that it doesn't affect the mode bits twice.
	unix.Umask(0)

	if *cmd == "" {
		server.Serve()
		waitOnMount(server)
		return
	}
	go server.Serve()
	waitOnMount(server)
	defer func() {
		server.Unmount()
		server.Wait()
	}()

	cmdArgs := strings.Split(strings.Trim(*cmd, "\""), " ")
	c := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		log.Warningf(err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}
