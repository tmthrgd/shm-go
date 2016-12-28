// Copyright 2016 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License license that can be found in
// the LICENSE file.

package shm

import "errors"

var (
	ErrInvalidSharedMemory = errors.New("invalid shared memory")
	ErrNotMultipleOf64     = errors.New("blockSize is not a multiple of 64")
	ErrInvalidBuffer       = errors.New("invalid buffer")
)
