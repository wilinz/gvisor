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

package filter_fuzz_test

import (
	"testing"

	"github.com/wilinz/gvisor/pkg/abi/linux"
	"github.com/wilinz/gvisor/pkg/seccomp"
	"github.com/wilinz/gvisor/pkg/sentry/platform/systrap"
	"github.com/wilinz/gvisor/runsc/boot/filter/config"
	"github.com/wilinz/gvisor/test/secfuzz"
)

// FuzzFilterOptimizationsResultInConsistentProgram tests that optimizations
// do not affect the behavior of the generated seccomp-bpf program.
func FuzzFilterOptimizationsResultInConsistentProgram(f *testing.F) {
	fuzzFilterOptimizationsResultInConsistentProgram(f)
}

// TestFilterOptimizationsResultInConsistentProgram tests that optimizations
// do not affect the behavior of the generated seccomp-bpf program, as a unit
// test. Unlike the fuzz-based test, it only operates on a static corpus.
// Still, it does enforce full branch coverage, so can be used as a quicker
// way to verify functionality without running a fuzz test for an interminate
// amount of time.
func TestFilterOptimizationsResultInConsistentProgram(t *testing.T) {
	fuzzFilterOptimizationsResultInConsistentProgram(&secfuzz.StaticCorpus{T: t})
}

func fuzzFilterOptimizationsResultInConsistentProgram(f secfuzz.FuzzLike) {
	f.Helper()
	filterOpts := config.Options{
		Platform: (&systrap.Systrap{}).SeccompInfo(),
	}
	rules, denyRules := config.Rules(filterOpts)
	ruleSets := []seccomp.RuleSet{
		{
			Rules:  denyRules,
			Action: linux.SECCOMP_RET_ERRNO,
		},
		{
			Rules:  rules,
			Action: linux.SECCOMP_RET_ALLOW,
		},
	}
	unoptimizedOpts := config.SeccompOptions(filterOpts)
	unoptimizedOpts.Optimize = false
	unoptimized, _, err := seccomp.BuildProgram(ruleSets, unoptimizedOpts)
	if err != nil {
		f.Fatalf("failed to build unoptimized program: %v", err)
	}
	fuzzeeUnoptimized := secfuzz.Fuzzee{
		Name:         "unoptimized",
		Instructions: unoptimized,

		// We cannot enforce full coverage on the unoptimized program,
		// because some of its checks are impossible to meet.
		// For example, it ends up checking things like
		// "if (A & 0) == 0" when checking both 32-bit halves of a
		// "masked equal" check, and the "false" branch of that can
		// never be covered.
		EnforceFullCoverage: false,
	}
	optimizedOpts := config.SeccompOptions(filterOpts)
	optimizedOpts.Optimize = true
	optimized, _, err := seccomp.BuildProgram(ruleSets, optimizedOpts)
	if err != nil {
		f.Fatalf("failed to build optimized program: %v", err)
	}
	fuzzeeOptimized := secfuzz.Fuzzee{
		Name:                "optimized",
		Instructions:        optimized,
		EnforceFullCoverage: true,
	}
	df, err := secfuzz.NewDiffFuzzer(f, &fuzzeeUnoptimized, &fuzzeeOptimized)
	if err != nil {
		f.Fatalf("failed to create diff fuzzer: %v", err)
	}
	df.DeriveCorpusFromRuleSets(ruleSets)
	df.Fuzz()
}
