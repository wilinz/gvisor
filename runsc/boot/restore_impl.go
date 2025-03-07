// Copyright 2024 The gVisor Authors.
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

//go:build !false
// +build !false

package boot

import (
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/wilinz/gvisor/pkg/sentry/control"
	"github.com/wilinz/gvisor/pkg/sentry/fsimpl/proc"
)

func preSaveImpl(*Loader, *control.SaveOpts) error {
	return nil
}

// Precondition: The kernel should be running.
func postRestoreImpl(*Loader) error {
	return nil
}

// Precondition: The kernel should be running.
func postResumeImpl(*Loader) error {
	return nil
}

func newProcInternalData(*specs.Spec) *proc.InternalData {
	return &proc.InternalData{}
}

func (l *Loader) kernelInitExtra() {}
