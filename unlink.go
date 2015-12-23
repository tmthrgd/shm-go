package main

/*
#include <stdlib.h>   // For free
#include <sys/mman.h> // For shm_*
*/
import "C"

import "unsafe"

func Unlink(name string) error {
	nameC := C.CString(name)
	defer C.free(unsafe.Pointer(nameC))

	_, err := C.shm_unlink(nameC)
	return err
}
