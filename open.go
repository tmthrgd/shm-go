package main

/*
#cgo LDFLAGS: -lrt

#include <stdlib.h>          // For free
#include <fcntl.h>           // For O_* constants
#include <sys/stat.h>        // For mode constants
#include <sys/mman.h>        // For shm_*

#include "structs.h"
*/
import "C"

import (
	"golang.org/x/sys/unix"
	"unsafe"
)

func OpenSimplex(name string) (ReadWriteCloser, error) {
	shmName := C.CString(name)
	defer C.free(unsafe.Pointer(shmName))

	fd, err := C.shm_open(shmName, C.O_RDWR, 0)

	if err != nil {
		return nil, err
	}

	defer unix.Close(int(fd))

	addr, err := C.mmap(nil, C.size_t(sharedHeaderSize), C.PROT_READ, C.MAP_SHARED, fd, 0)

	if err != nil {
		return nil, err
	}

	shared := (*C.shared_mem_t)(unsafe.Pointer(addr))
	blockCount, blockSize := shared.block_count, shared.block_size

	if _, err = C.munmap(addr, C.size_t(sharedHeaderSize)); err != nil {
		return nil, err
	}

	size := sharedHeaderSize + (blockHeaderSize+uintptr(blockSize))*uintptr(blockCount)
	addr, err = C.mmap(nil, C.size_t(size), C.PROT_READ|C.PROT_WRITE, C.MAP_SHARED, fd, 0)

	if err != nil {
		return nil, err
	}

	shared = (*C.shared_mem_t)(unsafe.Pointer(addr))

	var closed int64
	return &readWriter{
		shared: shared,
		size:   size,

		closed: &closed,
	}, nil
}

func OpenDuplex(name string) (Reader, WriteCloser, error) {
	shmName := C.CString(name)
	defer C.free(unsafe.Pointer(shmName))

	fd, err := C.shm_open(shmName, C.O_RDWR, 0)

	if err != nil {
		return nil, nil, err
	}

	defer unix.Close(int(fd))

	addr, err := C.mmap(nil, C.size_t(sharedHeaderSize), C.PROT_READ, C.MAP_SHARED, fd, 0)

	if err != nil {
		return nil, nil, err
	}

	shared := (*C.shared_mem_t)(unsafe.Pointer(addr))
	blockCount, blockSize := shared.block_count, shared.block_size

	if _, err = C.munmap(addr, C.size_t(sharedHeaderSize)); err != nil {
		return nil, nil, err
	}

	sharedSize := sharedHeaderSize + (blockHeaderSize+uintptr(blockSize))*uintptr(blockCount)
	addr, err = C.mmap(nil, C.size_t(2*sharedSize), C.PROT_READ|C.PROT_WRITE, C.MAP_SHARED, fd, 0)

	if err != nil {
		return nil, nil, err
	}

	var closed int64
	return &readWriter{
			shared: (*C.shared_mem_t)(unsafe.Pointer(uintptr(addr) + sharedSize)),

			closed: &closed,
		}, &readWriter{
			shared: (*C.shared_mem_t)(unsafe.Pointer(addr)),
			size:   2 * sharedSize,

			closed: &closed,
		}, nil
}
