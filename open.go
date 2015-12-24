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
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"unsafe"
)

func OpenSimplex(name string) (*ReadWriteCloser, error) {
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

	shared := (*C.shared_mem_t)(addr)
	blockCount, blockSize := uintptr(shared.block_count), uintptr(shared.block_size)

	if _, err = C.munmap(addr, C.size_t(sharedHeaderSize)); err != nil {
		return nil, err
	}

	size := sharedHeaderSize + (blockHeaderSize+blockSize)*blockCount
	addr, err = C.mmap(nil, C.size_t(size), C.PROT_READ|C.PROT_WRITE, C.MAP_SHARED, fd, 0)

	if err != nil {
		return nil, err
	}

	fmt.Fprintf(os.Stderr, "mmap mapped %d bytes to %p\n", size, addr)

	shared = (*C.shared_mem_t)(addr)
	return &ReadWriteCloser{
		readShared:  shared,
		writeShared: shared,
		size:        size,
	}, nil
}

func OpenDuplex(name string) (*ReadWriteCloser, error) {
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
	blockCount, blockSize := uintptr(shared.block_count), uintptr(shared.block_size)

	if _, err = C.munmap(addr, C.size_t(sharedHeaderSize)); err != nil {
		return nil, err
	}

	sharedSize := sharedHeaderSize + (blockHeaderSize+blockSize)*blockCount
	size := 2 * sharedSize

	addr, err = C.mmap(nil, C.size_t(size), C.PROT_READ|C.PROT_WRITE, C.MAP_SHARED, fd, 0)

	if err != nil {
		return nil, err
	}

	fmt.Fprintf(os.Stderr, "mmap mapped %d bytes to %p\n", size, addr)

	return &ReadWriteCloser{
		readShared:  (*C.shared_mem_t)(unsafe.Pointer(uintptr(addr) + sharedSize)),
		writeShared: (*C.shared_mem_t)(addr),
		size:        size,
	}, nil
}
