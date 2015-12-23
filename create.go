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

func Create(name string) (*ReadWriter, error) {
	nameC := C.CString(name)
	defer C.free(unsafe.Pointer(nameC))

	fd, err := C.shm_open(nameC, C.O_CREAT|C.O_EXCL|C.O_TRUNC|C.O_RDWR, 0644)

	if err != nil {
		return nil, err
	}

	defer unix.Close(int(fd))

	l := unsafe.Sizeof(C.shared_mem_t{})

	if err = unix.Ftruncate(int(fd), int64(l)); err != nil {
		return nil, err
	}

	addr, err := C.mmap(nil, C.size_t(l), C.PROT_READ|C.PROT_WRITE, C.MAP_SHARED, fd, 0)

	if err != nil {
		return nil, err
	}

	fmt.Fprintf(os.Stderr, "mmap allocated %d bytes at %p\n", l, addr)

	shared := (*C.shared_mem_t)(unsafe.Pointer(addr))

	if _, err = C.memset(addr, 0, C.size_t(l)); err != nil {
		return nil, err
	}

	/*
	 * memset already set:
	 *	shared.read_start, shared.read_end = 0, 0
	 *	block[i].size = 0
	 *	block[i].done_read, block[i].done_write = 0, 0
	 */
	shared.write_start, shared.write_end = 1, 1

	if err = sem_init(&shared.sem_signal, true, 0); err != nil {
		return nil, err
	}

	if err = sem_init(&shared.sem_avail, true, 0); err != nil {
		return nil, err
	}

	for i := 0; i < len(shared.blocks); i++ {
		switch i {
		case 0:
			shared.blocks[i].next, shared.blocks[i].prev = 1, C.longlong(len(shared.blocks)-1)
		case len(shared.blocks) - 1:
			shared.blocks[i].next, shared.blocks[i].prev = 0, C.longlong(len(shared.blocks)-2)
		default:
			shared.blocks[i].next, shared.blocks[i].prev = C.longlong(i+1), C.longlong(i-1)
		}
	}

	return &ReadWriter{
		shared: shared,
		len:    l,
		name:   name,
	}, nil
}
