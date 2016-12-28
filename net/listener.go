// Copyright 2016 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License license that can be found in
// the LICENSE file.

package net

import (
	"net"
	"os"
	"sync"

	"github.com/tmthrgd/shm-go"
)

type Listener struct {
	rw   *shm.ReadWriteCloser
	name string

	mut sync.Mutex
}

func Listen(name string, perm os.FileMode, blockCount, blockSize uint64) (*Listener, error) {
	rw, err := shm.CreateDuplex(name, perm, blockCount, blockSize)
	if err != nil {
		return nil, err
	}

	return &Listener{
		rw:   rw,
		name: name,
	}, nil
}

func NewListener(rw *shm.ReadWriteCloser, name string) *Listener {
	return &Listener{
		rw:   rw,
		name: name,
	}
}

func (l *Listener) Accept() (net.Conn, error) {
	l.mut.Lock()
	return &Conn{l.rw, l.name, &l.mut}, nil
}

func (l *Listener) Close() error {
	return l.rw.Close()
}

func (l *Listener) Addr() net.Addr {
	return addr(l.name)
}
