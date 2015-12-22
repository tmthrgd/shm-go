package main

/*
#cgo LDFLAGS: -lrt

#include <stdlib.h>          // For free
#include <string.h>          // For memset
#include <fcntl.h>           // For O_* constants
#include <sys/stat.h>        // For mode constants
#include <sys/mman.h>        // For shm_*
#include <semaphore.h>       // For sem_*

#define MAX_SHARED_DATA_LENGTH 8192

typedef struct {
	sem_t sem_read;
	sem_t sem_writen;
	long long written;
	char data[MAX_SHARED_DATA_LENGTH];
} shared_struct_t;
*/
import "C"

import (
	"flag"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
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
		fmt.Fprintf(ioutil.Discard, "%s failed with err: %v\n", name, err)
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

	l := unsafe.Sizeof(C.shared_struct_t{})

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
		fmt.Fprintf(ioutil.Discard, "shm_open returned: %d\n", fd)
	}

	addr, err := C.mmap(nil, C.size_t(l), C.PROT_READ|C.PROT_WRITE, C.MAP_SHARED, fd, 0)
	must("mmap", err)
	must("close", unix.Close(int(fd)))
	fmt.Fprintf(ioutil.Discard, "mmap of %d bytes returned: %v\n", l, addr)

	shared := (*C.shared_struct_t)(unsafe.Pointer(addr))
	data := (*[C.MAX_SHARED_DATA_LENGTH]byte)(unsafe.Pointer(&shared.data[0]))

	if isServer {
		err := sem_init(&shared.sem_read, true, 0)
		must("sem_init", err)

		err = sem_init(&shared.sem_writen, true, 0)
		must("sem_init", err)

		go func() {
			for {
				err := sem_wait(&shared.sem_writen)
				must("sem_wait", err)
				fmt.Fprintln(ioutil.Discard, "sem_wait returned")

				os.Stdout.Write(data[:shared.written])

				fmt.Fprintf(ioutil.Discard, "read %d bytes from data\n", shared.written)

				err = sem_post(&shared.sem_read)
				must("sem_post", err)
				fmt.Fprintln(ioutil.Discard, "sem_post returned")
			}
		}()

		// Termination
		// http://stackoverflow.com/a/18158859
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		signal.Notify(c, unix.SIGTERM)
		<-c

		err = sem_destroy(&shared.sem_read)
		must("sem_destroy", err)

		err = sem_destroy(&shared.sem_writen)
		must("sem_destroy", err)

		_, err = C.shm_unlink(shmName)
		must("shm_unlink", err)
	} else {
		var eof bool

		for {
			n, err := os.Stdin.Read(data[:])
			shared.written = C.longlong(n)

			if eof = err == io.EOF; !eof {
				must("os.Stdin.Read", err)
			}

			fmt.Fprintf(ioutil.Discard, "wrote %d bytes to data\n", n)

			if n > 0 {
				err = sem_post(&shared.sem_writen)
				must("sem_post", err)
				fmt.Fprintln(ioutil.Discard, "sem_post returned")

				if !eof {
					err = sem_wait(&shared.sem_read)
					must("sem_wait", err)
					fmt.Fprintln(ioutil.Discard, "sem_wait returned")
				}
			}

			if eof {
				break
			}
		}
	}

	_, err = C.munmap(addr, C.size_t(l))
	must("munmap", err)
}
