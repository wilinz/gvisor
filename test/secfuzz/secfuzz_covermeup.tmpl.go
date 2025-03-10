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

//go:build fuzz
// +build fuzz

package secfuzz

import (
	"github.com/wilinz/gvisor/pkg/bpf"
)

// Go does coverage-based fuzzing, so it discovers inputs that are
// "interesting" if they manage to cover new code.
// Go does not understand "BPF coverage", and there is no easy way to
// tell it that a certain BPF input has covered new lines of code.
// So... this approach converts BPF code coverage into native Go code
// coverage, by simply enumerating every single line of BPF code that
// could possibly exist, and having that be its own branch which Go's
// fuzzer then recognizes as being covered.
// This is possible because BPF programs are limited to
// `bpf.MaxInstructions` (currently 4,096), so all we need to do is to
// enumerate them all here.
// (Note that if this limit ends up being too small (which is possible;
// as the time of writing, our current unoptimized Sentry filters are
// around ~1,500 instructions), there is nothing preventing this
// file from being expanded to cover more instructions beyond this
// limit.)
//
// Then, because we want to compare the execution of two programs,
// we need to do it all over again; we can't reuse the same thing
// because this would mean that a line is considered "covered" by Go
// if *either* program covers it.
//
// This is hacky but works great!
//
// The actual `secfuzz_covermeup.go` file is generated by a genrule.

// countExecutedLinesProgram1 converts coverage data of the first BPF program
// to Go coverage data.
func countExecutedLinesProgram1(execution bpf.ExecutionMetrics, fuzzee *Fuzzee) {
	covered := execution.Coverage
	switch len(execution.Coverage) {
	// GENERATED_LINES_INSERTED_HERE_THIS_IS_A_LOAD_BEARING_COMMENT
	case 1:
		// The last switch statement is not auto-generated,
		// because unlike all other cases that are auto-generated
		// before it, it does not `fallthrough`.
		if covered[0] {
			fuzzee.coverage[0].Store(true)
		}
	}
}

// countExecutedLinesProgram2 converts coverage data of the second BPF
// program to Go coverage data.
func countExecutedLinesProgram2(execution bpf.ExecutionMetrics, fuzzee *Fuzzee) {
	covered := execution.Coverage
	switch len(execution.Coverage) {
	// GENERATED_LINES_INSERTED_HERE_THIS_IS_A_LOAD_BEARING_COMMENT
	case 1:
		// The last switch statement is not auto-generated,
		// because unlike all other cases that are auto-generated
		// before it, it does not `fallthrough`.
		if covered[0] {
			fuzzee.coverage[0].Store(true)
		}
	}
}
