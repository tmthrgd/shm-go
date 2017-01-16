// Copyright 2016 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License license that can be found in
// the LICENSE file.

package shm

import (
	"golang.org/x/sys/unix"
	"sync/atomic"
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

	if atomic.LoadUint32((*uint32)(&shared.Version)) != version {
		return nil, ErrInvalidSharedMemory
	}

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
		name: name,

		data:          data,
		readShared:    shared,
		writeShared:   shared,
		size:          size,
		fullBlockSize: blockHeaderSize + blockSize,

		Flags: (*[len(shared.Flags)]uint32)(unsafe.Pointer(&shared.Flags[0])),
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

	if atomic.LoadUint32((*uint32)(&shared.Version)) != version {
		return nil, ErrInvalidSharedMemory
	}

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

	writeShared := (*sharedMem)(unsafe.Pointer(&data[0]))
	return &ReadWriteCloser{
		name: name,

		data:          data,
		readShared:    (*sharedMem)(unsafe.Pointer(&data[sharedSize])),
		writeShared:   writeShared,
		size:          size,
		fullBlockSize: blockHeaderSize + blockSize,

		Flags: (*[len(writeShared.Flags)]uint32)(unsafe.Pointer(&writeShared.Flags[0])),
	}, nil
}
