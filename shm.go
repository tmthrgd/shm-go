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
	"golang.org/x/crypto/ssh/terminal"
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

var (
	ErrNotOwner          = errors.New("not owner of shared memory")
	ErrNotCloser         = errors.New("does not implement type of Closer")
	errInvalidBlockIndex = errors.New("invalid block index")
)

const shmName = "/shm-go"

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

	var interactive bool
	flag.BoolVar(&interactive, "i", false, "run an interactive client/server with duplex connections")

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

	if unlink {
		must("Unlink", Unlink(shmName))
		return
	}

	if !interactive {
		if isServer {
			reader, err := CreateSimplex(shmName, 1024, 8192)
			must("Create", err)

			go func() {
				for {
					_, err := io.Copy(os.Stdout, reader)
					must("io.Copy", err)
				}
			}()

			// Termination
			// http://stackoverflow.com/a/18158859
			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt, os.Kill, unix.SIGTERM)
			<-c

			must("reader.Close", reader.Close())
			must("Unlink", Unlink(shmName))
		} else {
			writer, err := OpenSimplex(shmName)
			must("Open", err)

			_, err = io.Copy(writer, os.Stdin)
			must("io.Copy", err)

			must("writer.Close", writer.Close())
		}

		return
	}

	var closer io.Closer

	done := make(chan struct{}, 1)

	if isServer {
		reader, writer, err := CreateDuplex(shmName, 1024, 8192)
		must("Create", err)
		closer = reader

		go func() {
			for {
				_, err := io.Copy(os.Stdout, io.TeeReader(reader, writer))
				must("io.Copy", err)
			}
		}()
	} else {
		reader, writer, err := OpenDuplex(shmName)
		must("Open", err)
		closer = writer

		oldState, err := terminal.MakeRaw(syscall.Stdin)
		must("terminal.MakeRaw", err)
		defer terminal.Restore(syscall.Stdin, oldState)

		term := terminal.NewTerminal(os.Stdin, ">")

		go func() {
			for {
				_, err := io.Copy(term, reader)
				must("io.Copy", err)
			}
		}()

		go func() {
			for {
				line, err := term.ReadLine()
				must("term.ReadLine", err)

				switch line {
				case "quit", "q":
					done <- struct{}{}
					return
				}

				_, err = io.WriteString(writer, line+"\n")
				must("io.WriteString", err)
			}
		}()
	}

	// Termination
	// http://stackoverflow.com/a/18158859
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill, unix.SIGTERM)

	select {
	case <-c:
	case <-done:
	}

	must("closer.Close", closer.Close())

	if isServer {
		must("Unlink", Unlink(shmName))
	}
}
