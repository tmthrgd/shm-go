package main

/*
#cgo LDFLAGS: -lrt

#include <stdlib.h>          // For free
#include <string.h>          // For memset
#include <fcntl.h>           // For O_* constants
#include <sys/stat.h>        // For mode constants
#include <sys/mman.h>        // For shm_*
#include <semaphore.h>       // For sem_*

#define IPC_BLOCK_COUNT 512
#define IPC_BLOCK_SIZE  8192

typedef struct {
	char data[IPC_BLOCK_SIZE];

	long long next;
	long long prev;

	volatile int done_read;
	volatile int done_write;

	volatile long long size;
} shared_block_t;

typedef struct {
	shared_block_t blocks[IPC_BLOCK_COUNT];

	volatile long long read_start;
	volatile long long read_end;

	volatile long long write_start;
	volatile long long write_end;

	sem_t sem_signal;
	sem_t sem_avail;
} shared_mem_t;
*/
import "C"

import (
	"flag"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"unsafe"
)

func must(name string, err error) {
	if err != nil {
		if err, ok := err.(syscall.Errno); ok && err == 0 {
			return
		}

		panic(fmt.Sprintf("%s failed with err: %v\n", name, err))
	}
}

func should(name string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s failed with err: %v\n", name, err)
	}
}

func getWriteBlock(shared *C.shared_mem_t) (*C.shared_block_t, error) {
	for {
		blockIndex := atomic.LoadInt64((*int64)(&shared.write_start))
		block := &shared.blocks[blockIndex]

		if int64(block.next) == atomic.LoadInt64((*int64)(&shared.read_end)) {
			if err := sem_wait(&shared.sem_avail); err != nil {
				return nil, err
			}

			continue
		}

		if atomic.CompareAndSwapInt64((*int64)(&shared.write_start), blockIndex, int64(block.next)) {
			return block, nil
		}
	}
}

func putWriteBlock(shared *C.shared_mem_t, block *C.shared_block_t) error {
	atomic.StoreInt32((*int32)(&block.done_write), 1)

	for {
		blockIndex := atomic.LoadInt64((*int64)(&shared.write_end))
		block := &shared.blocks[blockIndex]

		if !atomic.CompareAndSwapInt32((*int32)(&block.done_write), 1, 0) {
			return nil
		}

		atomic.CompareAndSwapInt64((*int64)(&shared.write_end), blockIndex, int64(block.next))

		if int64(block.prev) == atomic.LoadInt64((*int64)(&shared.read_start)) {
			if err := sem_post(&shared.sem_signal); err != nil {
				return err
			}
		}
	}
}

func getReadBlock(shared *C.shared_mem_t) (*C.shared_block_t, error) {
	for {
		blockIndex := atomic.LoadInt64((*int64)(&shared.read_start))
		block := &shared.blocks[blockIndex]

		if int64(block.next) == atomic.LoadInt64((*int64)(&shared.write_end)) {
			if err := sem_wait(&shared.sem_signal); err != nil {
				return nil, err
			}

			continue
		}

		if atomic.CompareAndSwapInt64((*int64)(&shared.read_start), blockIndex, int64(block.next)) {
			return block, nil
		}
	}
}

func putReadBlock(shared *C.shared_mem_t, block *C.shared_block_t) error {
	atomic.StoreInt32((*int32)(&block.done_read), 1)

	for {
		blockIndex := atomic.LoadInt64((*int64)(&shared.read_end))
		block := &shared.blocks[blockIndex]

		fmt.Fprintf(os.Stderr, "in putReadBlock with block between %d...%d\n", block.prev, block.next)

		if !atomic.CompareAndSwapInt32((*int32)(&block.done_read), 1, 0) {
			return nil
		}

		atomic.CompareAndSwapInt64((*int64)(&shared.read_end), blockIndex, int64(block.next))

		// this might not be atomic (enough)
		if int64(block.prev) == atomic.LoadInt64((*int64)(&shared.write_start)) {
			if err := sem_post(&shared.sem_avail); err != nil {
				return err
			}
		}
	}
}

func main() {
	var role string
	flag.StringVar(&role, "role", "server", "server/client")
	var unlink bool
	flag.BoolVar(&unlink, "unlink", false, "unlink shared memory")
	flag.Parse()

	var isServer = role == "server"

	switch role {
	case "server", "client":
	default:
		flag.PrintDefaults()
		return
	}

	shmName := C.CString("/shm-go")
	defer C.free(unsafe.Pointer(shmName))

	l := unsafe.Sizeof(C.shared_mem_t{})

	var fd C.int

	if isServer {
		if unlink {
			_, err := C.shm_unlink(shmName)
			must("shm_unlink", err)
			return
		}

		var err error
		fd, err = C.shm_open(shmName, C.O_CREAT|C.O_EXCL|C.O_TRUNC|C.O_RDWR, 0644)
		must("shm_open", err)

		must("ftruncate", unix.Ftruncate(int(fd), int64(l)))
	} else {
		var err error
		fd, err = C.shm_open(shmName, C.O_RDWR, 0)
		must("shm_open", err)
		fmt.Fprintf(os.Stderr, "shm_open returned: %d\n", fd)
	}

	addr, err := C.mmap(nil, C.size_t(l), C.PROT_READ|C.PROT_WRITE, C.MAP_SHARED, fd, 0)
	must("mmap", err)
	must("close", unix.Close(int(fd)))
	fmt.Fprintf(os.Stderr, "mmap of %d bytes returned: %v\n", l, addr)

	shared := (*C.shared_mem_t)(unsafe.Pointer(addr))

	if isServer {
		_, err = C.memset(addr, 0, C.size_t(l))
		must("memset", err)

		/*
		 * memset already set:
		 *	shared.read_start, shared.read_end = 0, 0
		 *	block[i].size = 0
		 *	block[i].done_read, block[i].done_write = 0, 0
		 */
		shared.write_start, shared.write_end = 1, 1

		err = sem_init(&shared.sem_signal, true, 0)
		must("sem_init", err)

		err = sem_init(&shared.sem_avail, true, 0)
		must("sem_init", err)

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

		go func() {
			for {
				block, err := getReadBlock(shared)
				must("getReadBlock", err)
				fmt.Fprintf(os.Stderr, "getReadBlock returned between %d..%d\n", block.prev, block.next)

				data := (*[len(block.data)]byte)(unsafe.Pointer(&block.data[0]))

				os.Stdout.Write(data[:block.size])

				fmt.Fprintf(os.Stderr, "read %d bytes from data\n", block.size)

				must("putReadBlock", putReadBlock(shared, block))
				fmt.Fprintln(os.Stderr, "putReadBlock returned")
			}
		}()

		// Termination
		// http://stackoverflow.com/a/18158859
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		signal.Notify(c, unix.SIGTERM)
		<-c

		err = sem_destroy(&shared.sem_signal)
		must("sem_destroy", err)

		err = sem_destroy(&shared.sem_avail)
		must("sem_destroy", err)

		_, err = C.shm_unlink(shmName)
		must("shm_unlink", err)
	} else {
		var eof bool

		for {
			block, err := getWriteBlock(shared)
			must("getWriteBlock", err)
			fmt.Fprintf(os.Stderr, "getWriteBlock returned between %d..%d\n", block.prev, block.next)

			data := (*[len(block.data)]byte)(unsafe.Pointer(&block.data[0]))

			n, err := os.Stdin.Read(data[:])
			block.size = C.longlong(n)

			if eof = err == io.EOF; !eof {
				must("os.Stdin.Read", err)
			}

			fmt.Fprintf(os.Stderr, "wrote %d bytes to data\n", n)

			must("putWriteBlock", putWriteBlock(shared, block))
			fmt.Fprintln(os.Stderr, "putWriteBlock returned")

			if eof {
				break
			}
		}
	}

	_, err = C.munmap(addr, C.size_t(l))
	must("munmap", err)
}
