package main

type Addr string

func (Addr) Network() string {
	return "shm"
}

func (a Addr) String() string {
	return string(a)
}
