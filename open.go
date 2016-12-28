package shm

import (
	"golang.org/x/sys/unix"
	"unsafe"
)

func OpenSimplex(name string) (*ReadWriteCloser, error) {
	file, err := shmOpen(name, unix.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	data, err := unix.Mmap(int(file.Fd()), 0, sharedHeaderSize, unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}

	shared := (*sharedMem)(unsafe.Pointer(&data[0]))
	blockCount, blockSize := uint64(shared.BlockCount), uint64(shared.BlockSize)

	if err = unix.Munmap(data); err != nil {
		return nil, err
	}

	size := sharedHeaderSize + (blockHeaderSize+blockSize)*blockCount

	data, err = unix.Mmap(int(file.Fd()), 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}

	shared = (*sharedMem)(unsafe.Pointer(&data[0]))
	return &ReadWriteCloser{
		data:          data,
		readShared:    shared,
		writeShared:   shared,
		size:          size,
		fullBlockSize: blockHeaderSize + blockSize,
	}, nil
}

func OpenDuplex(name string) (*ReadWriteCloser, error) {
	file, err := shmOpen(name, unix.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	data, err := unix.Mmap(int(file.Fd()), 0, sharedHeaderSize, unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}

	shared := (*sharedMem)(unsafe.Pointer(&data[0]))
	blockCount, blockSize := uint64(shared.BlockCount), uint64(shared.BlockSize)

	if err = unix.Munmap(data); err != nil {
		return nil, err
	}

	sharedSize := sharedHeaderSize + (blockHeaderSize+blockSize)*blockCount
	size := 2 * sharedSize

	data, err = unix.Mmap(int(file.Fd()), 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}

	return &ReadWriteCloser{
		data:          data,
		readShared:    (*sharedMem)(unsafe.Pointer(uintptr(unsafe.Pointer(&data[0])) + uintptr(sharedSize))),
		writeShared:   (*sharedMem)(unsafe.Pointer(&data[0])),
		size:          size,
		fullBlockSize: blockHeaderSize + blockSize,
	}, nil
}
