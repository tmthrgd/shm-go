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

func Open(name string) (*ReadWriter, error) {
	shmName := C.CString(name)
	defer C.free(unsafe.Pointer(shmName))

	fd, err := C.shm_open(shmName, C.O_RDWR, 0)

	if err != nil {
		return nil, err
	}

	defer unix.Close(int(fd))

	l := unsafe.Sizeof(C.shared_mem_t{})
	addr, err := C.mmap(nil, C.size_t(l), C.PROT_READ|C.PROT_WRITE, C.MAP_SHARED, fd, 0)

	if err != nil {
		return nil, err
	}

	shared := (*C.shared_mem_t)(unsafe.Pointer(addr))
	return &ReadWriter{
		shared: shared,
		len:    l,
		name:   "",
	}, nil
}
