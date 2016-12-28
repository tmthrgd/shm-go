// Copyright 2016 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License license that can be found in
// the LICENSE file.

package net

import (
	"net"
	"sync"
	"time"

	"github.com/tmthrgd/shm-go"
)

type Conn struct {
	*shm.ReadWriteCloser
	name string

	mut *sync.Mutex
}

func (c *Conn) Close() error {
	c.mut.Unlock()
	return nil
}

func (c *Conn) LocalAddr() net.Addr {
	return addr(c.name)
}

func (c *Conn) RemoteAddr() net.Addr {
	return addr(c.name)
}

func (c *Conn) SetDeadline(t time.Time) error {
	return nil
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	return nil
}
