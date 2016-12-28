// Copyright 2016 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License license that can be found in
// the LICENSE file.

package net

type addr string

func (addr) Network() string {
	return "shm"
}

func (a addr) String() string {
	return string(a)
}
