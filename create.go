package main

/*
#include <stdlib.h>          // For free
#include <string.h>          // For memset
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

func CreateSimplex(name string, blockCount, blockSize int64) (ReadWriteCloser, error) {
	if blockSize&0x3f != 0 {
		return nil, fmt.Errorf("blockSize of %d is not a multiple of 64", blockSize)
	}

	fullBlockSize := blockHeaderSize + uintptr(blockSize)
	size := sharedHeaderSize + fullBlockSize*uintptr(blockCount)

	if size > 1<<30 {
		return nil, fmt.Errorf("invalid total memory size of %d, maximum allowed is %d", size, 1<<30)
	}

	nameC := C.CString(name)
	defer C.free(unsafe.Pointer(nameC))

	fd, err := C.shm_open(nameC, C.O_CREAT|C.O_EXCL|C.O_TRUNC|C.O_RDWR, 0644)

	if err != nil {
		return nil, err
	}

	defer unix.Close(int(fd))

	if err = unix.Ftruncate(int(fd), int64(size)); err != nil {
		return nil, err
	}

	addr, err := C.mmap(nil, C.size_t(size), C.PROT_READ|C.PROT_WRITE, C.MAP_SHARED, fd, 0)

	if err != nil {
		return nil, err
	}

	fmt.Fprintf(os.Stderr, "mmap allocated %d bytes at %p\n", size, addr)

	if _, err = C.memset(addr, 0, C.size_t(size)); err != nil {
		return nil, err
	}

	shared := (*C.shared_mem_t)(unsafe.Pointer(addr))

	/*
	 * memset already set:
	 *	shared.read_start, shared.read_end = 0, 0
	 *	block[i].size = 0
	 *	block[i].done_read, block[i].done_write = 0, 0
	 */
	shared.block_count, shared.block_size = C.longlong(blockCount), C.longlong(blockSize)

	shared.write_start, shared.write_end = 1, 1

	if err = sem_init(&shared.sem_signal, true, 0); err != nil {
		return nil, err
	}

	if err = sem_init(&shared.sem_avail, true, 0); err != nil {
		return nil, err
	}

	for i := int64(0); i < blockCount; i++ {
		block := (*C.shared_block_t)(unsafe.Pointer(uintptr(addr) + sharedHeaderSize + uintptr(i)*fullBlockSize))

		switch i {
		case 0:
			block.next, block.prev = 1, C.longlong(blockCount-1)
		case blockCount - 1:
			block.next, block.prev = 0, C.longlong(blockCount-2)
		default:
			block.next, block.prev = C.longlong(i+1), C.longlong(i-1)
		}
	}

	var closed int64
	return &readWriter{
		shared: shared,
		size:   size,

		closed: &closed,
	}, nil
}

func CreateDuplex(name string, blockCount, blockSize int64) (ReadCloser, Writer, error) {
	if blockSize&0x3f != 0 {
		return nil, nil, fmt.Errorf("blockSize of %d is not a multiple of 64", blockSize)
	}

	fullBlockSize := blockHeaderSize + uintptr(blockSize)
	sharedSize := sharedHeaderSize + fullBlockSize*uintptr(blockCount)
	size := 2 * sharedSize

	if size > 1<<30 {
		return nil, nil, fmt.Errorf("invalid total memory size of %d, maximum allowed is %d", size, 1<<30)
	}

	nameC := C.CString(name)
	defer C.free(unsafe.Pointer(nameC))

	fd, err := C.shm_open(nameC, C.O_CREAT|C.O_EXCL|C.O_TRUNC|C.O_RDWR, 0644)

	if err != nil {
		return nil, nil, err
	}

	defer unix.Close(int(fd))

	if err = unix.Ftruncate(int(fd), int64(size)); err != nil {
		return nil, nil, err
	}

	addr, err := C.mmap(nil, C.size_t(size), C.PROT_READ|C.PROT_WRITE, C.MAP_SHARED, fd, 0)

	if err != nil {
		return nil, nil, err
	}

	fmt.Fprintf(os.Stderr, "mmap allocated %d bytes at %p\n", size, addr)

	if _, err = C.memset(addr, 0, C.size_t(size)); err != nil {
		return nil, nil, err
	}

	for i := 0; i < 2; i++ {
		shared := (*C.shared_mem_t)(unsafe.Pointer(uintptr(addr) + uintptr(i)*sharedSize))

		/*
		 * memset already set:
		 *	shared.read_start, shared.read_end = 0, 0
		 *	shared.blocks[i].size = 0
		 *	shared.blocks[i].done_read, shared.blocks[i].done_write = 0, 0
		 */
		shared.block_count, shared.block_size = C.longlong(blockCount), C.longlong(blockSize)

		shared.write_start, shared.write_end = 1, 1

		if err = sem_init(&shared.sem_signal, true, 0); err != nil {
			return nil, nil, err
		}

		if err = sem_init(&shared.sem_avail, true, 0); err != nil {
			return nil, nil, err
		}

		for j := int64(0); j < blockCount; j++ {
			block := (*C.shared_block_t)(unsafe.Pointer(uintptr(unsafe.Pointer(shared)) + sharedHeaderSize + uintptr(j)*fullBlockSize))

			switch j {
			case 0:
				block.next, block.prev = 1, C.longlong(blockCount-1)
			case blockCount - 1:
				block.next, block.prev = 0, C.longlong(blockCount-2)
			default:
				block.next, block.prev = C.longlong(j+1), C.longlong(j-1)
			}
		}
	}

	var closed int64
	return &readWriter{
			shared: (*C.shared_mem_t)(unsafe.Pointer(addr)),
			size:   size,

			closed: &closed,
		}, &readWriter{
			shared: (*C.shared_mem_t)(unsafe.Pointer(uintptr(addr) + sharedSize)),

			closed: &closed,
		}, nil
}
