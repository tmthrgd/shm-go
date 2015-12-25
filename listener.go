package main

import (
	"net"
	"sync"
)

type Listener struct {
	rw   *ReadWriteCloser
	name string

	mut sync.Mutex
}

func Listen(name string, blockCount, blockSize int64) (*Listener, error) {
	rw, err := CreateDuplex(name, blockCount, blockSize)

	if err != nil {
		return nil, err
	}

	return &Listener{
		rw:   rw,
		name: name,
	}, nil
}

func NewListener(rw *ReadWriteCloser, name string) *Listener {
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
	return Addr(l.name)
}
