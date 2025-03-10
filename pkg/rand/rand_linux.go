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

package rand

import (
	"bufio"
	"crypto/rand"
	"io"

	"golang.org/x/sys/unix"
	"github.com/wilinz/gvisor/pkg/sync"
)

// reader implements an io.Reader that returns pseudorandom bytes.
type reader struct {
	once         sync.Once
	useGetrandom bool
}

// Read implements io.Reader.Read.
func (r *reader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		_, err := unix.Getrandom(p, 0)
		if err != unix.ENOSYS {
			r.useGetrandom = true
		}
	})

	if r.useGetrandom {
		return unix.Getrandom(p, 0)
	}
	return rand.Read(p)
}

// bufferedReader implements a threadsafe buffered io.Reader.
type bufferedReader struct {
	mu sync.Mutex
	r  *bufio.Reader
}

// Read implements io.Reader.Read.
func (b *bufferedReader) Read(p []byte) (int, error) {
	// In Linux, reads of up to page size bytes will always complete fully.
	// See drivers/char/random.c:get_random_bytes_user().
	// NOTE(gvisor.dev/issue/9445): Some applications rely on this behavior.
	const pageSize = 4096
	min := len(p)
	if min > pageSize {
		min = pageSize
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return io.ReadAtLeast(b.r, p, min)
}

// Reader is the default reader.
var Reader io.Reader = &bufferedReader{r: bufio.NewReader(&reader{})}

// Read reads from the default reader.
func Read(b []byte) (int, error) {
	return io.ReadFull(Reader, b)
}

// Init can be called to make sure /dev/urandom is pre-opened on kernels that
// do not support getrandom(2).
func Init() error {
	p := make([]byte, 1)
	_, err := Read(p)
	return err
}
