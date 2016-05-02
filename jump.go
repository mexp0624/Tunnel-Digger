// This is a simple SOCKS5 proxy server.
// Copyright 2013-2015, physacco. Distributed under the MIT license.
// Copyright 2016, mexp0624. Distributed under the MIT license.

package main

import (
	"io"
	"sync"
	"time"
//	"fmt"
	"log"
	"net"
	"flag"
	"runtime"
	"math/rand"
	"strconv"
)


var hubAddr = flag.String("hub", "45.32.24.156", "")
var hubPort = flag.String("p", "1080", "")
var to = flag.String("to", "127.0.0.1:25565", "")

// MUX Commands
const (
	NEW byte = 0
	FIN byte = 1
	CLS byte = 2
	ERR byte = 4
	PNG byte = 8
	ACK byte = 9
)

const (
	MAX_LEN = 65535
	MAX_BUFF = 32*1024
)


type Socket struct {
	id int
	mux *Mux
	isClose bool
	inEv chan int
	inBuffer []byte
	appendBuff sync.Mutex
	stat sync.Mutex
}

func(s *Socket) Write(b []byte) (n int, err error){
	dlen := len(b)
	defer func(){
		select {
			case <- s.mux.ping:
			default:
		}
		s.mux.ping <- false
	}()
	for {
		if dlen > MAX_LEN{
			s.mux.wMutex.Lock()
			s.mux.io.Write([]byte{ byte(s.id), byte(MAX_LEN >> 8), byte(MAX_LEN & 0xFF)})
			n2, err := s.mux.io.Write(b[:MAX_LEN])
			s.mux.wMutex.Unlock()
			b = b[MAX_LEN:]
			dlen -= MAX_LEN
			n += n2;
			if err != nil {
				return n, err
			}
		}else{
			s.mux.wMutex.Lock()
			s.mux.io.Write([]byte{ byte(s.id), byte(dlen >> 8), byte(dlen & 0xFF)})
			n, err = s.mux.io.Write(b)
			s.mux.wMutex.Unlock()
			return n, err
		}
	}
	return n, err
}
func(s *Socket) Read(b []byte) (n int, err error){
	<- s.inEv
	n = len(b)
	s.appendBuff.Lock()
	in := len(s.inBuffer)
	if in <= n {
		copy(b, s.inBuffer[:in])
		s.inBuffer = make([]byte, 0)
		s.appendBuff.Unlock()
		n = in
		s.stat.Lock()
		if s.isClose != false {
			s.stat.Unlock()
			return n, io.EOF
		}else{
			s.stat.Unlock()
			return n, nil
		}
	}else{
		copy(b, s.inBuffer[:n])
		rlen := in - n
		s.inBuffer = s.inBuffer[n:n+rlen]
		if rlen > 0 {
			select {
				case <- s.inEv:
				default:
			}
			s.inEv <- rlen
			s.appendBuff.Unlock()
			return n, nil
		}else{
			s.inBuffer = make([]byte, 0)
			s.appendBuff.Unlock()
			s.stat.Lock()
			if s.isClose != false {
				s.stat.Unlock()
				return n, io.EOF
			}else{
				s.stat.Unlock()
				return n, nil
			}
		}
	}
}
func(s *Socket) Push(b []byte) (n int, err error){
	n = len(b)

	s.appendBuff.Lock()
	s.inBuffer = append(s.inBuffer, b[0:n]...)
	select {
		case <- s.inEv:
		default:
	}
	s.inEv <- n
	s.appendBuff.Unlock()

	for {
		s.appendBuff.Lock()
		lbuf := len(s.inBuffer)
		s.appendBuff.Unlock()
		if lbuf > MAX_BUFF {
	        time.Sleep(5 * time.Millisecond)
		}else{
			s.appendBuff.Lock()
			lbuf = len(s.inBuffer)
			tmp := make([]byte, lbuf)
			copy(tmp, s.inBuffer)
			s.inBuffer = tmp
			s.appendBuff.Unlock()
			break
		}
	}
	return n, nil
}
func(s *Socket) Close() error{
	log.Printf("[ch%d][Close]%v\n", s.id, s.isClose)
	s.stat.Lock()
	if !s.isClose {
		s.stat.Unlock()
		s.mux.wMutex.Lock()
		s.mux.io.Write([]byte{ byte(0xFF), byte(s.id), CLS})
		s.mux.wMutex.Unlock()
		s.stat.Lock()
		s.isClose = true;
		s.stat.Unlock()
		s.mux.poolMutex.Lock()
		if s.mux.pool[s.id] != nil {
			s.mux.pool[s.id] = nil
		}
		s.mux.poolMutex.Unlock()
	}else{
		s.stat.Unlock()
	}

	s.appendBuff.Lock()
//	s.inBuffer = make([]byte, 0)
	select {
		case <- s.inEv:
		default:
	}
	s.inEv <- -1
	s.appendBuff.Unlock()

    return nil
}
func (s *Socket) LocalAddr() net.Addr {
	return s.mux.io.LocalAddr()
}

func (s *Socket) RemoteAddr() net.Addr {
	return s.mux.io.RemoteAddr()
}
func (s *Socket) SetDeadline(deadline time.Time) (err error) {
	if err = s.SetReadDeadline(deadline); err != nil {
		return
	}
	if err = s.SetWriteDeadline(deadline); err != nil {
		return
	}
	return
}

func (socket *Socket) SetReadDeadline(dl time.Time) error {
	return nil
}

func (socket *Socket) SetWriteDeadline(dl time.Time) error {
	return nil
}


func mkch(ch_id int, mux *Mux) (*Socket, error) {
	socket := &Socket {
		id: ch_id,
		mux: mux,
		inEv: make(chan int, 1),
		inBuffer: make([]byte, 0),
		isClose: false,
	}
//	log.Println("[mk new ch]:", ch_id, mux, socket)
//	log.Println("[mk new ch]:", ch_id)
	return socket, nil
}

type Mux struct {
	pool []*Socket
	poolMutex sync.Mutex
	io net.Conn
	wMutex sync.Mutex
	ping chan bool
}

func mkmux(mux net.Conn) (*Mux, error) {
	m := &Mux {
		pool: make([]*Socket, 255),
		io: mux,
		ping: make(chan bool, 1),
	}
	log.Println("[mk mux]:", mux)
	return m, nil
}

func readBytesAsync(conn io.Reader, count int) (buf []byte, err error) {
	c := make(chan error)
	b := make([]byte, count)

	go func() {
		i := 0
		rem := count
		for {
//			log.Printf("[readBytesAsync]: %d, %d, %d", i, i+rem, count)
			n, err := conn.Read(b[i:i+rem])
			if n > 0 {
				rem -= n
				i += n
				if rem == 0{
					c <- err
					return
				}
			}
			if err != nil {
//				log.Println("[readBytesAsync]err:", err)
				c <- err
				return
			}
		}

	}()

	err = <- c
//	log.Println("[readBytesAsync]return:", count, err)
	return b, err
}


func safeXORBytes(dst, a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		dst[i] = a[i] ^ b[i]
	}
	return n
}
func hashBytes(dst, a []byte, init uint32) int {
	n := len(a)
	var hash uint32 = init
	for i := 0; i < n; i++ {
		hash = ((hash << 13) | (hash >> 19) ) + uint32(a[i])
	}
	dst[0] = dst[0] ^ byte((hash >> 24) & 0xFF)
	dst[1] = dst[1] ^ byte((hash >> 16) & 0xFF)
	dst[2] = dst[2] ^ byte((hash >> 8) & 0xFF)
	dst[3] = dst[3] ^ byte(hash & 0xFF)
	return n
}

func getId() (id []byte) {
	id = make([]byte, 10)
	interfaces, err :=  net.Interfaces()
	if err != nil {
		return
	}
	count := len(interfaces)
	for _, inter := range interfaces {
		hashBytes(id[6:10], append(inter.HardwareAddr, []byte(inter.Name)...), uint32(count))
		safeXORBytes(id[0:6], id[0:6], inter.HardwareAddr)
	}
	return
}

func copyContent(from net.Conn, to net.Conn, complete chan bool, done chan bool, otherDone chan bool, name string) {
	var err error = nil
	var bytes []byte = make([]byte, 16*1024)
	var read int = 0
	for {
		select {
		case <- otherDone:
			complete <- true
			return
		default:
//			from.SetReadDeadline(time.Now().Add(time.Second * 1))
			read, err = from.Read(bytes)
			if err != nil {
//				log.Println("[copyContent]read:", name, read, err)
				complete <- true
				done <- true
				return
			}
//			to.SetWriteDeadline(time.Now().Add(time.Second * 1))
			_, err = to.Write(bytes[:read])
			if err != nil {
//				log.Println("[copyContent]write:", name, err)
				complete <- true
				done <- true
				return
			}
		}
	}
}

func handleConnection(frontconn net.Conn, ch int) {
	frontaddr := frontconn.RemoteAddr().String() + "-" + strconv.Itoa(ch)
	log.Println("new connection", frontaddr)
	defer func() {
		if err := recover(); err != nil {
			log.Println("connection error", frontaddr, err)
		}
		frontconn.Close()
		log.Println("disconnected", frontaddr)
	}()


	log.Printf("trying to connect to %s...\n", *to)
	loacl, err := net.Dial("tcp", *to)
	if err != nil {
		log.Printf("[E1-0]failed to connect to %s: %s\n", *to, err)
		return
	}

	loacladdr := loacl.RemoteAddr().String()
	log.Println("connected loacl server", loacl, loacladdr)
	defer func() {
		loacl.Close()
		log.Println("disconnected loacl server", loacl, loacladdr)
	}()

    complete := make(chan bool, 2)
    ch1 := make(chan bool, 1)
    ch2 := make(chan bool, 1)
    go copyContent(frontconn, loacl, complete, ch1, ch2, "r -> s")
    go copyContent(loacl, frontconn, complete, ch2, ch1, "s -> r")

	<- complete
//	<- complete
}

func hubConn() {

	rConn, err := net.Dial("tcp", *hubAddr + ":" + *hubPort)
	if err != nil {
		log.Print("[E0-1]", err)
		return
	}
	defer rConn.Close()

//	log.Printf("to server : %s\n", rConn.RemoteAddr())

	rConn.Write(getId())
	version, err := readBytesAsync(rConn, 4)
	if err != nil {
		log.Println("[E0-2][to hub error]", err)
		return
	}
	if version[0] != 0x00 || version[1] != 0x00 {
		log.Printf("[E0-3]need update!!\n")
		return
	}
	if version[2] == 0x00 || version[3] == 0x00 {
		log.Printf("[E0-4]server full!!\n")
		return
	}
	port := (int(version[2]) << 8) | int(version[3])
	log.Printf("[I0-0]Connect to %s:%d\n", *hubAddr, port)


	mx, _ := mkmux(rConn)
	pool := mx.pool

	quit := make(chan int)
	go func() {
		next := time.Duration(10 + rand.Intn(25))
		t := time.NewTimer(next * time.Second)
		for{
			select {
				case <- quit:
					t.Stop()
					return
				case <- mx.ping:
					t.Stop()
				case <-t.C:
					// send ping
//					log.Printf("[send][cmd][ping]\n")
					mx.wMutex.Lock()
					mx.io.Write([]byte{ byte(0xFF), byte(0xFF), PNG})
					mx.wMutex.Unlock()
			}
			next = time.Duration(10 + rand.Intn(25))
			t = time.NewTimer(next * time.Second)
		}
	}()
	defer func() {
		quit <- 1
	}()

	for {
		head, err := readBytesAsync(rConn, 3)
		if err != nil {
			log.Println("[I0-1][to hub close]", err)
			return
		}
		select {
			case <- mx.ping:
			default:
		}
		mx.ping <- false

//		log.Println("[head]", head[0], head[1], head[2])
		if head[0] == 0xFF { // CMD
			ch := int(head[1])
			mx.poolMutex.Lock()
			switch head[2] {
				case NEW:
					log.Printf("[ch%d][cmd][new]\n", ch)
					if pool[ch] == nil {
						con, _ := mkch(ch, mx)
						pool[ch] = con
						go handleConnection(net.Conn(con), ch)
					}
				case FIN:
//					log.Printf("[ch%d][cmd][end]\n", ch)
					if pool[ch] != nil {
						pool[ch].stat.Lock()
						pool[ch].isClose = true;
						pool[ch].stat.Unlock()
						pool[ch].Close()
						pool[ch] = nil
					}
				case CLS:
					log.Printf("[ch%d][cmd][close]\n", ch)
					if pool[ch] != nil {
						pool[ch].stat.Lock()
						pool[ch].isClose = true;
						pool[ch].stat.Unlock()
						pool[ch].Close()
						pool[ch] = nil
					}
				case ERR:
					log.Printf("[ch%d][cmd][err]\n", ch)
					if pool[ch] != nil {
						pool[ch].stat.Lock()
						pool[ch].isClose = true;
						pool[ch].stat.Unlock()
						pool[ch].Close()
						pool[ch] = nil
					}
				case PNG:
//					log.Printf("[ch%d][cmd][ping]\n", ch)
					mx.wMutex.Lock()
					mx.io.Write([]byte{ byte(0xFF), byte(ch), ACK})
					mx.wMutex.Unlock()

				case ACK:
//					log.Printf("[ch%d][cmd][ack]\n", ch)
			}
			mx.poolMutex.Unlock()
		} else { // DATA
			ch := int(head[0])
			dlen := (int(head[1]) << 8) | int(head[2])
//			log.Printf("[ch%d][data]%d\n", ch, dlen)
			mx.poolMutex.Lock()
			if pool[ch] != nil {
				data, err := readBytesAsync(rConn, dlen)
//				log.Printf("[ch%d][data]r %d %v\n", ch, dlen, err)
				if err != nil {
					log.Println("[E0-5][to hub close']", err)
					pool[ch].Close()
					pool[ch] = nil
					return
				}
				_, err = pool[ch].Push(data)
//				n, err := pool[ch].Push(data)
//				log.Printf("[ch%d][data]w %d\n", ch, n)
				// TODO: buffer overflow?
				if err != nil {
					pool[ch].Close()
					pool[ch] = nil
					break
				}
				mx.poolMutex.Unlock()
			}else{
				mx.poolMutex.Unlock()
				log.Printf("[ch%d][not found]: %d\n", ch, dlen)
				_, err = readBytesAsync(rConn, dlen)
				if err != nil {
					log.Println("[E0-6][to hub close'']", err)
					return
				}
			}
		}
	}
}


func main() {
	log.SetFlags(log.Ldate|log.Ltime|log.Lshortfile)
	flag.Parse()

	runtime.GOMAXPROCS(runtime.NumCPU())
//	runtime.GOMAXPROCS(1)

	rand.Seed(int64(time.Now().Nanosecond()))
//	for {
		hubConn()
//		time.Sleep(3 * time.Second)
//	}
}

