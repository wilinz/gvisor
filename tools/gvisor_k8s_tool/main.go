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

// gvisor_k8s_tool is a command-line tool to interact with gVisor in
// Kubernetes clusters.
package main

import (
	"context"
	"os"

	"github.com/google/subcommands"
	"github.com/wilinz/gvisor/runsc/flag"
	"github.com/wilinz/gvisor/tools/gvisor_k8s_tool/cmd/install"
)

func registerCommands() {
	subcommands.Register(subcommands.HelpCommand(), "")
	subcommands.Register(subcommands.FlagsCommand(), "")
	subcommands.Register(new(install.Command), "install")
}

func main() {
	registerCommands()
	flag.Parse()
	switch subcommands.Execute(context.Background()) {
	case subcommands.ExitSuccess:
		os.Exit(0)
	default:
		os.Exit(128)
	}
}
