// Copyright 2018 The gVisor Authors.
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

// Package contexttest builds a test context.Context.
package contexttest

import (
	"os"
	"testing"
	"time"

	"github.com/wilinz/gvisor/pkg/atomicbitops"
	"github.com/wilinz/gvisor/pkg/context"
	"github.com/wilinz/gvisor/pkg/memutil"
	"github.com/wilinz/gvisor/pkg/sentry/kernel/auth"
	"github.com/wilinz/gvisor/pkg/sentry/ktime"
	"github.com/wilinz/gvisor/pkg/sentry/limits"
	"github.com/wilinz/gvisor/pkg/sentry/pgalloc"
	"github.com/wilinz/gvisor/pkg/sentry/platform"
	"github.com/wilinz/gvisor/pkg/sentry/platform/ptrace"
	"github.com/wilinz/gvisor/pkg/sentry/uniqueid"
)

// Context returns a Context that may be used in tests. Uses ptrace as the
// platform.Platform.
//
// Note that some filesystems may require a minimal kernel for testing, which
// this test context does not provide. For such tests, see kernel/contexttest.
func Context(tb testing.TB) context.Context {
	const memfileName = "contexttest-memory"
	memfd, err := memutil.CreateMemFD(memfileName, 0)
	if err != nil {
		tb.Fatalf("error creating application memory file: %v", err)
	}
	memfile := os.NewFile(uintptr(memfd), memfileName)
	mf, err := pgalloc.NewMemoryFile(memfile, pgalloc.MemoryFileOpts{
		DisableMemoryAccounting: true,
	})
	if err != nil {
		memfile.Close()
		tb.Fatalf("error creating pgalloc.MemoryFile: %v", err)
	}
	p, err := ptrace.New()
	if err != nil {
		tb.Fatal(err)
	}
	// Test usage of context.Background is fine.
	return &TestContext{
		Context:     context.Background(),
		l:           limits.NewLimitSet(),
		mf:          mf,
		platform:    p,
		creds:       auth.NewAnonymousCredentials(),
		otherValues: make(map[any]any),
	}
}

// TestContext represents a context with minimal functionality suitable for
// running tests.
type TestContext struct {
	context.Context
	l           *limits.LimitSet
	mf          *pgalloc.MemoryFile
	platform    platform.Platform
	creds       *auth.Credentials
	otherValues map[any]any
}

// globalUniqueID tracks incremental unique identifiers for tests.
var globalUniqueID atomicbitops.Uint64

// globalUniqueIDProvider implements unix.UniqueIDProvider.
type globalUniqueIDProvider struct{}

// UniqueID implements unix.UniqueIDProvider.UniqueID.
func (*globalUniqueIDProvider) UniqueID() uint64 {
	return globalUniqueID.Add(1)
}

// lastInotifyCookie is a monotonically increasing counter for generating unique
// inotify cookies.
var lastInotifyCookie atomicbitops.Uint32

// hostClock implements ktime.SampledClock.
type hostClock struct {
	ktime.WallRateClock
	ktime.NoClockEvents
}

// Now implements ktime.Clock.Now.
func (*hostClock) Now() ktime.Time {
	return ktime.FromNanoseconds(time.Now().UnixNano())
}

// SupportsTimers implements ktime.Clock.Now.
func (*hostClock) SupportsTimers() bool {
	return true
}

// NewTimer implements ktime.Clock.NewTimer.
func (c *hostClock) NewTimer(l ktime.Listener) ktime.Timer {
	return ktime.NewSampledTimer(c, l)
}

// RegisterValue registers additional values with this test context. Useful for
// providing values from external packages that contexttest can't depend on.
func (t *TestContext) RegisterValue(key, value any) {
	t.otherValues[key] = value
}

// Value implements context.Context.
func (t *TestContext) Value(key any) any {
	switch key {
	case auth.CtxCredentials:
		return t.creds
	case limits.CtxLimits:
		return t.l
	case pgalloc.CtxMemoryFile:
		return t.mf
	case platform.CtxPlatform:
		return t.platform
	case uniqueid.CtxGlobalUniqueID:
		return (*globalUniqueIDProvider).UniqueID(nil)
	case uniqueid.CtxGlobalUniqueIDProvider:
		return &globalUniqueIDProvider{}
	case uniqueid.CtxInotifyCookie:
		return lastInotifyCookie.Add(1)
	case ktime.CtxRealtimeClock:
		return &hostClock{}
	default:
		if val, ok := t.otherValues[key]; ok {
			return val
		}
		return t.Context.Value(key)
	}
}

// RootContext returns a Context that may be used in tests that need root
// credentials. Uses ptrace as the platform.Platform.
func RootContext(tb testing.TB) context.Context {
	return auth.ContextWithCredentials(Context(tb), auth.NewRootCredentials(auth.NewRootUserNamespace()))
}

// WithLimitSet returns a copy of ctx carrying l.
func WithLimitSet(ctx context.Context, l *limits.LimitSet) context.Context {
	return limitContext{ctx, l}
}

type limitContext struct {
	context.Context
	l *limits.LimitSet
}

// Value implements context.Context.
func (lc limitContext) Value(key any) any {
	switch key {
	case limits.CtxLimits:
		return lc.l
	default:
		return lc.Context.Value(key)
	}
}
