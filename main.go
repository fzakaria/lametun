package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"syscall"
	"unsafe"
)

const (
	// https://elixir.bootlin.com/linux/v5.8.10/source/include/uapi/linux/if.h#L33
	IFNAMSIZ = 16
	// sizeof(struct ifreq)
	IfReqSize = 40
	// https://github.com/golang/go/blob/50bd1c4d4eb4fac8ddeb5f063c099daccfb71b26/src/syscall/zerrors_linux_amd64.go#L1183
	TUNSETIFF = 0x400454ca
	// https://github.com/golang/go/blob/50bd1c4d4eb4fac8ddeb5f063c099daccfb71b26/src/syscall/zerrors_linux_amd64.go#L1183
	IFF_TUN         = 0x1
	IfReqFlagOffset = IFNAMSIZ
)

// let's open the TUN device
// A tun device is a bit wonky in that you have to first open "/dev/net/tun"
// then run a IOCTL syscall to turn the fd returned for the desired network tun device.
// This code makes use of some unsafe golang code, this is merely to avoid pulling in
// dependencies since this is for demonstration
func openTunDevice(dev string) (*os.File, error) {
	file, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	// IOCTL for TUN requires the ifreq struct
	// https://elixir.bootlin.com/linux/v5.8.10/source/include/uapi/linux/if.h#L234
	// we fill in the required struct members such as the device name & that it is a TUN
	var ifr [IfReqSize]byte
	copy(ifr[:], dev)
	ifr[IfReqFlagOffset] = IFF_TUN

	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		file.Fd(),
		uintptr(TUNSETIFF),
		uintptr(unsafe.Pointer(&ifr[0])),
	)

	if errno != 0 {
		return nil, errno
	}

	return file, nil
}

func main() {
	port := flag.Int("port", 1234, "The protocol port for lametun")
	dev := flag.String("device", "tun0", "The TUN device name")
	listen := flag.Bool("listen", false, "Whether to designate this machine as the server")
	server := flag.String("server", "", "The server to connect to")
	flag.Parse()

	fmt.Printf("listen:%v server:%v dev:%v port:%v\n", *listen, *server, *dev, *port)

	if *listen && *server != "" {
		fmt.Fprintf(os.Stderr, "Cannot listen and set server flag\n")
		os.Exit(1)
	}

	if !*listen && *server == "" {
		fmt.Fprintf(os.Stderr, "You must specify the server or mark this host to listen\n")
		os.Exit(1)
	}

	tun, err := openTunDevice(*dev)
	if err != nil {
		panic(err)
	}

	var conn *net.UDPConn
	if *listen {
		conn, err = net.ListenUDP("udp4", &net.UDPAddr{Port: *port})
		if err != nil {
			panic(err)
		}
	} else {
		raddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", server, port))
		if err != nil {
			panic(err)
		}
		conn, err = net.DialUDP("udp4", nil, raddr)
		if err != nil {
			panic(err)
		}
	}
	defer conn.Close()

	quit := make(chan struct{})
	go func() {
		// we make sure to pick a buffer size at least greater than our MTU
		// 2048 is much larger :)
		buffer := make([]byte, 2048)
		for {
			bytes, _, err := conn.ReadFromUDP(buffer)
			if err != nil {
				break
			}

			// write to the tun device
			_, err = tun.Write(buffer[:bytes])
			if err != nil {
				break
			}
		}

		// signal to terminate
		quit <- struct{}{}
	}()

	go func() {
		for {
			// we make sure to pick a buffer size at least greater than our MTU
			// 2048 is much larger :)
			buffer := make([]byte, 2048)

			bytes, err := tun.Read(buffer)
			if err != nil {
				break
			}

			// at this point the buffer is a complete UDP packet; let's forward it to our UDP peer
			_, err = conn.WriteTo(buffer[:bytes], nil)
			if err != nil {
				break
			}
		}

		// signal to terminate
		quit <- struct{}{}
	}()

	// wait until an error is given
	<-quit
}
