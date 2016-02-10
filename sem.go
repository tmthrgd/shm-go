package main

/*
#include <semaphore.h>      // For sem_*
#include <linux/futex.h>    // For FUTEX_*
#include <bits/local_lim.h> // For SEM_VALUE_MAX

// This is pulled from glibc-2.17/nptl/sysdeps/unix/sysv/linux/internaltypes.h
struct new_sem
{
	unsigned int value;
	int private;
	unsigned long int nwaiters;
};
*/
import "C"

import (
	"golang.org/x/sys/unix"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// This mirrors atomic_decrement_if_positive from glibc-2.17/include/atomic.h
func atomicDecrementIfPositive(mem *uint32) uint32 {
	for {
		if old := atomic.LoadUint32(mem); old == 0 || atomic.CompareAndSwapUint32(mem, old, old-1) {
			return old
		}
	}
}

// This (mostly?) mirrors __new_sem_wait from glibc-2.17/nptl/sysdeps/unix/sysv/linux/sem_wait.c
func sem_wait(sem *C.sem_t) error {
	isem := (*C.struct_new_sem)(unsafe.Pointer(sem))

	if atomicDecrementIfPositive((*uint32)(&isem.value)) > 0 {
		return nil
	}

	atomic.AddUint64((*uint64)(&isem.nwaiters), 1)

	for {
		//err = do_futex_wait(isem);
		if _, _, err := unix.Syscall6(unix.SYS_FUTEX, uintptr(unsafe.Pointer(&isem.value)), uintptr(C.FUTEX_WAIT), 0, 0, 0, 0); err != 0 && err != syscall.EWOULDBLOCK {
			atomic.AddUint64((*uint64)(&isem.nwaiters), ^uint64(0))
			return err
		}

		if atomicDecrementIfPositive((*uint32)(&isem.value)) > 0 {
			atomic.AddUint64((*uint64)(&isem.nwaiters), ^uint64(0))
			return nil
		}
	}
}

// This (loosely?) mirrors __new_sem_trywait from glibc-2.17/nptl/sysdeps/unix/sysv/linux/sem_trywait.c
func sem_trywait(sem *C.sem_t) error {
	isem := (*C.struct_new_sem)(unsafe.Pointer(sem))

	if atomicDecrementIfPositive((*uint32)(&isem.value)) > 0 {
		return nil
	}

	return syscall.EAGAIN
}

// This mirrors __new_sem_post from glibc-2.17/nptl/sysdeps/unix/sysv/linux/sem_post.c
func sem_post(sem *C.sem_t) error {
	isem := (*C.struct_new_sem)(unsafe.Pointer(sem))

	for {
		cur := atomic.LoadUint32((*uint32)(&isem.value))

		if cur == C.SEM_VALUE_MAX {
			return syscall.EOVERFLOW
		}

		if atomic.CompareAndSwapUint32((*uint32)(&isem.value), cur, cur+1) {
			break
		}
	}

	// atomic_full_barrier ();

	if atomic.LoadUint64((*uint64)(&isem.nwaiters)) <= 0 {
		return nil
	}

	if _, _, err := unix.Syscall6(unix.SYS_FUTEX, uintptr(unsafe.Pointer(&isem.value)), uintptr(C.FUTEX_WAKE), 1, 0, 0, 0); err != 0 {
		return err
	}

	return nil
}

// This mirrors __new_sem_init from glibc-2.17/nptl/sem_init.c
func sem_init(sem *C.sem_t, pshared bool, value uint32) error {
	if value > C.SEM_VALUE_MAX {
		return syscall.EINVAL
	}

	// This does NOT mirror __new_sem_init but non-shared semaphores are not used
	if !pshared {
		return syscall.ENOSYS
	}

	isem := (*C.struct_new_sem)(unsafe.Pointer(sem))
	isem.value = C.uint(value)
	isem.private = 0
	isem.nwaiters = 0

	return nil
}

// This mirrors __new_sem_destroy from glibc-2.17/nptl/sem_destroy.c
func sem_destroy(sem *C.sem_t) error {
	return nil
}
