package main

import (
	"flag"
	"fmt"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"unsafe"
)

const (
	// sizeof(struct ifreq)
	IfReqSize = 40
)

// let's open the TUN device
// A tun device is a bit wonky in that you have to first open "/dev/net/tun"
// then run a IOCTL syscall to turn the fd returned for the desired network tun device.
// This code makes use of some unsafe golang code, this is merely to avoid pulling in
// dependencies since this is for demonstration
func openTunDevice(dev string) (*os.File, error) {
	fd, err := unix.Open("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	// IOCTL for TUN requires the ifreq struct
	// https://elixir.bootlin.com/linux/v5.8.10/source/include/uapi/linux/if.h#L234
	// we fill in the required struct members such as the device name & that it is a TUN
	var ifr [IfReqSize]byte
	copy(ifr[:], dev)
	*(*uint16)(unsafe.Pointer(&ifr[unix.IFNAMSIZ])) = unix.IFF_TUN

	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.TUNSETIFF),
		uintptr(unsafe.Pointer(&ifr[0])),
	)

	if errno != 0 {
		return nil, fmt.Errorf("error syscall.Ioctl(): %v\n", errno)
	}

	unix.SetNonblock(fd, true)
	return os.NewFile(uintptr(fd), "/dev/net/tun"), nil
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
		raddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", *server, *port))
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
				fmt.Fprintf(os.Stderr, "error reading from UDP connection: %v\n", err)
				break
			}

			fmt.Printf("Writing %d bytes to the tun device.\n", bytes)

			// write to the tun device
			_, err = tun.Write(buffer[:bytes])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error writing to tun: %v\n", err)
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
				fmt.Fprintf(os.Stderr, "error reading from tun: %v\n", err)
				break
			}

			fmt.Printf("Read %d bytes from the tun device.\n", bytes)

			// at this point the buffer is a complete UDP packet; let's forward it to our UDP peer
			_, err = conn.WriteTo(buffer[:bytes], nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error writing to UDP connection: %v\n", err)
				break
			}
		}

		// signal to terminate
		quit <- struct{}{}
	}()

	// wait until an error is given
	<-quit
}
