package shm

import (
	"errors"
	"net"
	"sync"
)

type Dialer struct {
	rw   *ReadWriteCloser
	name string

	mut sync.Mutex
}

func Dial(name string) (net.Conn, error) {
	rw, err := OpenDuplex(name)
	if err != nil {
		return nil, err
	}

	return (&Dialer{
		rw:   rw,
		name: name,
	}).Dial("shm", name)
}

func NewDialer(rw *ReadWriteCloser, name string) *Dialer {
	return &Dialer{
		rw:   rw,
		name: name,
	}
}

func (d *Dialer) Dial(network, address string) (net.Conn, error) {
	if network != "shm" {
		return nil, errors.New("unrecognised network")
	}

	if address != d.name {
		return nil, errors.New("invalid address")
	}

	d.mut.Lock()
	return &Conn{d.rw, d.name, &d.mut}, nil
}
