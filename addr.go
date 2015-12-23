package main

type Addr struct {
	name string
}

func (*Addr) Network() string {
	return "shm"
}

func (a *Addr) String() string {
	return a.name
}
