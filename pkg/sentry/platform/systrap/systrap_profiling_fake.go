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

//go:build !systrap_profiling
// +build !systrap_profiling

package systrap

import (
	"github.com/wilinz/gvisor/pkg/metric"
)

// SystrapProfiling is a builder that produces conditionally compiled metrics.
// Metrics made from this are compiled and active at runtime when the
// "systrap_profiling" go-tag is specified at compilation.
var SystrapProfiling = metric.FakeMetricBuilder{}

//go:nosplit
func updateDebugMetrics(stubBoundLat, sentryBoundLat cpuTicks) {}
