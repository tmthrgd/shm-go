package main

/*
#cgo LDFLAGS: -lrt

#include "structs.h"
*/
import "C"

import (
	"errors"
	"flag"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/signal"
	"syscall"
	"unsafe"
)

const (
	sharedHeaderSize = unsafe.Sizeof(C.shared_mem_t{})
	blockHeaderSize  = unsafe.Sizeof(C.shared_block_t{})
)

var ErrNotOwner = errors.New("not owner of shared memory")

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

	if isServer {
		reader, err := Create("/shm-go", 1024, 8192)
		must("Create", err)
		defer func() {
			must("reader.Close", reader.Close())
		}()

		go func() {
			for {
				_, err := io.Copy(os.Stdout, reader)
				must("io.Copy", err)
			}
		}()

		// Termination
		// http://stackoverflow.com/a/18158859
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		signal.Notify(c, unix.SIGTERM)
		<-c

		must("reader.Unlink", reader.Unlink())
	} else {
		writer, err := Open("/shm-go")
		must("Open", err)
		defer func() {
			must("writer.Close", writer.Close())
		}()

		_, err = io.Copy(writer, os.Stdin)
		must("io.Copy", err)
	}
}
