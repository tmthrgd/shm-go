package shm

type addr string

func (addr) Network() string {
	return "shm"
}

func (a addr) String() string {
	return string(a)
}
