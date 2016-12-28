// Copyright 2016 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License license that can be found in
// the LICENSE file.

package main

import (
	"crypto/aes"
	"encoding/binary"
	"flag"
	"fmt"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/sys/unix"
	"hash/crc32"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"

	"github.com/tmthrgd/shm-go"
	shmNet "github.com/tmthrgd/shm-go/net"
)

const shmName = "/shm-go"

func must(name string, err error) {
	if err != nil {
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

	var httpdemo bool
	flag.BoolVar(&httpdemo, "http", false, "run a http server")

	var noop bool
	flag.BoolVar(&noop, "noop", false, "send blocks without writing to them")

	var enc bool
	flag.BoolVar(&enc, "enc", false, "stream ctr encrypted zeros through the connection")

	var num uint64
	flag.Uint64Var(&num, "c", 1<<35, "num of bytes (for -noop and -enc)")

	var unlink bool
	flag.BoolVar(&unlink, "unlink", false, "unlink shared memory")

	flag.Parse()

	isServer := role == "server"

	switch role {
	case "server", "client":
	default:
		flag.PrintDefaults()
		return
	}

	if unlink {
		must("Unlink", shm.Unlink(shmName))
		return
	}

	f, err := os.Create("cpu-" + role + ".prof")
	must("os.Create", err)
	must("pprof.StartCPUProfile", pprof.StartCPUProfile(f))
	defer pprof.StopCPUProfile()

	switch {
	case interactive:
		var closer io.Closer

		done := make(chan struct{})

		if isServer {
			rw, err := shm.CreateDuplex(shmName, 1024, 8192)
			must("Create", err)
			closer = rw

			go func() {
				for {
					_, err := io.Copy(os.Stdout, io.TeeReader(rw, rw))
					must("io.Copy", err)
				}
			}()
		} else {
			rw, err := shm.OpenDuplex(shmName)
			must("Open", err)
			closer = rw

			oldState, err := terminal.MakeRaw(syscall.Stdin)
			must("terminal.MakeRaw", err)
			defer terminal.Restore(syscall.Stdin, oldState)

			term := terminal.NewTerminal(os.Stdin, "> ")

			go func() {
				for {
					_, err := io.Copy(term, rw)
					must("io.Copy", err)
				}
			}()

			go func() {
				for {
					line, err := term.ReadLine()
					must("term.ReadLine", err)

					switch line {
					case "quit", "q":
						close(done)
						return
					}

					_, err = io.WriteString(rw, line+"\n")
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
			must("Unlink", shm.Unlink(shmName))
		}
	case httpdemo:
		var closer io.Closer

		done := make(chan struct{})

		if isServer {
			rw, err := shm.CreateDuplex(shmName, 1024, 8192)
			must("Create", err)
			closer = rw

			http.HandleFunc("/foo", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintln(w, "hello from go land")
			})

			http.HandleFunc("/bar", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, "Hello, %q\n", html.EscapeString(r.URL.Path))
			})

			ln := shmNet.NewListener(rw, shmName)

			go func() {
				// TODO(tmthrgd): More efficiant shared memory http server
				must("http.Serve", http.Serve(ln, nil))
			}()

		} else {
			rw, err := shm.OpenDuplex(shmName)
			must("OpenDuplex", err)
			closer = rw

			tr := &http.Transport{
				Dial: func(n, a string) (net.Conn, error) {
					return shmNet.NewDialer(rw, shmName).Dial("shm", shmName)
				},
			}

			//tr.RegisterProtocol("shm", )

			// TODO(tmthrgd): More efficiant shared memory http client
			client := &http.Client{
				Transport: tr,
			}

			oldState, err := terminal.MakeRaw(syscall.Stdin)
			must("terminal.MakeRaw", err)
			defer terminal.Restore(syscall.Stdin, oldState)

			term := terminal.NewTerminal(os.Stdin, "> ")

			base := &url.URL{
				Scheme: "http",
				Host:   "localhost",
			}

			go func() {
				for {
					line, err := term.ReadLine()
					must("term.ReadLine", err)

					switch line {
					case "quit", "q":
						close(done)
						return
					}

					u, err := base.Parse(line)
					must("base.Parse", err)

					resp, err := client.Get(u.String())
					must("client.Get", err)

					err = resp.Write(os.Stdout)
					must("resp.Write", err)
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
			must("Unlink", shm.Unlink(shmName))
		}
	case noop:
		if isServer {
			reader, err := shm.CreateSimplex(shmName, 1024, 8192)
			must("Create", err)

			go func() {
				for {
					buf, err := reader.GetReadBuffer()
					must("reader.GetReadBuffer", err)

					must("reader.SendReadBuffer", reader.SendReadBuffer(buf))
				}
			}()

			// Termination
			// http://stackoverflow.com/a/18158859
			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt, os.Kill, unix.SIGTERM)
			<-c

			must("reader.Close", reader.Close())
			must("Unlink", shm.Unlink(shmName))
		} else {
			writer, err := shm.OpenSimplex(shmName)
			must("Open", err)

			for i := uint64(0); i < num; {
				buf, err := writer.GetWriteBuffer()
				must("writer.GetWriteBuffer", err)

				buf.Data = buf.Data[:cap(buf.Data)]

				n, err := writer.SendWriteBuffer(buf)
				must("writer.SendWriteBuffer", err)

				i += uint64(n)
			}

			must("writer.Close", writer.Close())
		}
	case enc:
		var key [16]byte
		c, err := aes.NewCipher(key[:])
		must("aes.NewCipher", err)

		var block [8192]byte

		var crc uint32
		castagnoli := crc32.MakeTable(crc32.Castagnoli)

		if isServer {
			reader, err := shm.CreateSimplex(shmName, 1024, 8192)
			must("Create", err)

			go func() {
				for {
					buf, err := reader.GetReadBuffer()
					must("reader.GetReadBuffer", err)

					c.Decrypt(block[:], buf.Data)

					crc = crc32.Update(crc, castagnoli, buf.Data[:8])

					if buf.Flags[0] & 0x01 != 0 {
						if expect := binary.LittleEndian.Uint32(buf.Flags[1:]); crc == expect {
							fmt.Fprintf(os.Stderr, "valid final crc of: %d\n", crc)
						} else {
							fmt.Fprintf(os.Stderr, "invalid final crc of: %d, expected: %d\n", crc, expect)
						}

						crc = 0
					}

					must("reader.SendReadBuffer", reader.SendReadBuffer(buf))
				}
			}()

			// Termination
			// http://stackoverflow.com/a/18158859
			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt, os.Kill, unix.SIGTERM)
			<-c

			must("reader.Close", reader.Close())
			must("Unlink", shm.Unlink(shmName))
		} else {
			writer, err := shm.OpenSimplex(shmName)
			must("Open", err)

			for i := 0; i < len(block); i += 8 {
				binary.LittleEndian.PutUint64(block[i:], num)
			}

			for i := uint64(0); i < num; {
				buf, err := writer.GetWriteBuffer()
				must("writer.GetWriteBuffer", err)

				buf.Data = buf.Data[:cap(buf.Data)]
				c.Encrypt(buf.Data, block[:])

				crc = crc32.Update(crc, castagnoli, buf.Data[:8])

				if i+uint64(len(buf.Data)) < num {
					buf.Flags[0] &^= 0x1
				} else {
					// EOF
					buf.Flags[0] |= 0x1

					binary.LittleEndian.PutUint32(buf.Flags[1:], crc)
				}

				n, err := writer.SendWriteBuffer(buf)
				must("writer.SendWriteBuffer", err)

				i += uint64(n)
			}

			must("writer.Close", writer.Close())

			fmt.Fprintf(os.Stderr, "final crc: %d\n", crc)
		}
	default:
		if isServer {
			reader, err := shm.CreateSimplex(shmName, 1024, 8192)
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
			must("Unlink", shm.Unlink(shmName))
		} else {
			writer, err := shm.OpenSimplex(shmName)
			must("Open", err)

			_, err = io.Copy(writer, os.Stdin)
			must("io.Copy", err)

			must("writer.Close", writer.Close())
		}
	}
}
