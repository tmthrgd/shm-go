package main

import (
	"net"
	"sync"
	"time"
)

type Conn struct {
	*ReadWriteCloser
	name string

	mut *sync.Mutex
}

func (c *Conn) Close() error {
	c.mut.Unlock()
	return nil
}

func (c *Conn) LocalAddr() net.Addr {
	return Addr(c.name)
}

func (c *Conn) RemoteAddr() net.Addr {
	return Addr(c.name)
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
