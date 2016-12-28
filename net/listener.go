package net

import (
	"net"
	"sync"

	"github.com/tmthrgd/shm-go"
)

type Listener struct {
	rw   *shm.ReadWriteCloser
	name string

	mut sync.Mutex
}

func Listen(name string, blockCount, blockSize uint64) (*Listener, error) {
	rw, err := shm.CreateDuplex(name, blockCount, blockSize)
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
