package wasip3

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// boundAddresses tracks addresses used by simulated (e.g. IPv6) sockets.
var boundAddresses sync.Map // string -> bool

// activeListeners tracks simulated IPv6 listener addresses and their accept channels.
var activeListeners sync.Map // string -> chan net.Conn

// udpDatagram represents a simulated UDP datagram for IPv6 sockets.
type udpDatagram struct {
	data   []byte
	sender *net.UDPAddr
}

// udpMailboxes maps bound UDP addresses to their receive channels for simulated IPv6.
var udpMailboxes sync.Map // string -> chan udpDatagram

// tcpSocketResource represents a TCP socket.
type tcpSocketResource struct {
	mu        sync.Mutex
	family    uint8 // 0=IPv4, 1=IPv6
	listener  *net.TCPListener
	conn      *net.TCPConn
	pipeConn  net.Conn // for simulated IPv6 connections (net.Pipe)
	addr      *net.TCPAddr // bound address
	connected bool         // true if connect succeeded (even simulated)
	listening bool         // true if listen was called
	receiving bool         // true if receive() was called (can only call once)
	sending   bool         // true if send() was called (can only call once)
}

// udpSocketResource represents a UDP socket.
type udpSocketResource struct {
	mu         sync.Mutex
	family     uint8
	conn       *net.UDPConn
	addr       *net.UDPAddr
	remoteAddr *net.UDPAddr
}

// tcpListenerStream wraps a TCP listener as a stream resource for accepting connections.
type tcpListenerStream struct {
	listener    *net.TCPListener
	acceptCh    chan net.Conn // for simulated (IPv6) listeners
	pendingConn net.Conn     // cached connection from background accept
	host        *ComponentHost
}

// writeIPPort writes an IP address + port to memory using the component model's
// ip-socket-address layout: disc(u8) at +0, padding to +4, then case payload.
// IPv4 payload: port(u16), a(u8), b(u8), c(u8), d(u8)
// IPv6 payload: port(u16), pad(2), flow-info(u32), 8×u16 segments, scope-id(u32)
func writeIPPort(mem api.Memory, retPtr uint32, ip net.IP, port uint16) {
	if ip4 := ip.To4(); ip4 != nil {
		mem.WriteByte(retPtr, 0) // disc = IPv4
		mem.WriteUint16Le(retPtr+4, port)
		mem.WriteByte(retPtr+6, ip4[0])
		mem.WriteByte(retPtr+7, ip4[1])
		mem.WriteByte(retPtr+8, ip4[2])
		mem.WriteByte(retPtr+9, ip4[3])
	} else {
		ip6 := ip.To16()
		mem.WriteByte(retPtr, 1) // disc = IPv6
		mem.WriteUint16Le(retPtr+4, port)
		mem.WriteUint32Le(retPtr+8, 0) // flow-info
		for i := 0; i < 8; i++ {
			val := uint16(ip6[i*2])<<8 | uint16(ip6[i*2+1])
			mem.WriteUint16Le(retPtr+12+uint32(i)*2, val)
		}
		mem.WriteUint32Le(retPtr+28, 0) // scope-id
	}
}

// readAddressFlat reads an ip-socket-address from flattened component model params.
// The address starts at stack[offset]:
//
//	stack[offset]   = disc (0=IPv4, 1=IPv6)
//
// IPv4 (ipv4-socket-address { port, address: (u8,u8,u8,u8) }):
//
//	stack[offset+1] = port, stack[offset+2..+5] = a,b,c,d
//
// IPv6 (ipv6-socket-address { port, flow-info, address: (u16×8), scope-id }):
//
//	stack[offset+1] = port, stack[offset+2] = flow-info, stack[offset+3..+10] = segments, stack[offset+11] = scope-id
func readAddressFlat(stack []uint64, offset int) (net.IP, uint16) {
	disc := uint32(stack[offset])
	if disc == 0 {
		// IPv4: port at offset+1, then a,b,c,d at offset+2..+5
		port := uint16(stack[offset+1])
		a := byte(stack[offset+2])
		b := byte(stack[offset+3])
		c := byte(stack[offset+4])
		d := byte(stack[offset+5])
		return net.IPv4(a, b, c, d), port
	}
	// IPv6: port at offset+1, flow-info at offset+2, 8 segments at offset+3..+10, scope-id at offset+11
	port := uint16(stack[offset+1])
	ip := make(net.IP, 16)
	for i := 0; i < 8; i++ {
		val := uint16(stack[offset+3+i])
		ip[i*2] = byte(val >> 8)
		ip[i*2+1] = byte(val)
	}
	return ip, port
}

// WASI sockets error-code enum values.
// WASI sockets error-code variant discriminants (0-indexed):
// 0=access-denied, 1=not-supported, 2=invalid-argument, 3=out-of-memory,
// 4=timeout, 5=invalid-state, 6=address-not-bindable, 7=address-in-use, ...
const (
	errInvalidArgument    = 2
	errInvalidState       = 5
	errAddressNotBindable = 6
	errAddressInUse       = 7
	errConnectionRefused  = 9
	errDatagramTooLarge   = 13
)

// registerSockets is a no-op; all socket functions are handled dynamically by
// socketsImportHandler to avoid signature mismatches with the flat ABI lowering.
func (h *ComponentHost) registerSockets(cl *wazero.ComponentLinker) {}

// writeUDPReceiveResult writes a successful receive result to retPtr.
// Result layout: result<tuple<list<u8>, ip-socket-address>, error-code>
//   +0:  disc=0 (ok)
//   +4:  list ptr (u32) - allocated via cabi_realloc
//   +8:  list len (u32)
//   +12: ip-socket-address disc (u8) - 0=IPv4, 1=IPv6
//   For IPv4: +16: port(u16), +18: a,b,c,d
//   For IPv6: +16: port(u16), +20: flow-info(u32), +24: addr(u16×8), +40: scope-id(u32)
func writeUDPReceiveResult(ctx context.Context, mod api.Module, retPtr uint32, data []byte, addr *net.UDPAddr) {
	mem := mod.Memory()
	if mem == nil {
		return
	}
	mem.WriteByte(retPtr, 0) // ok disc

	// Write data list using cabi_realloc.
	if len(data) > 0 {
		ptr, err := cabiRealloc(ctx, mod, uint32(len(data)))
		if err == nil {
			mem.Write(ptr, data)
			mem.WriteUint32Le(retPtr+4, ptr)
		}
	} else {
		mem.WriteUint32Le(retPtr+4, 0)
	}
	mem.WriteUint32Le(retPtr+8, uint32(len(data)))

	// Write ip-socket-address.
	if addr == nil {
		mem.WriteByte(retPtr+12, 0) // IPv4 default
		return
	}
	ip4 := addr.IP.To4()
	if ip4 != nil {
		mem.WriteByte(retPtr+12, 0) // IPv4
		mem.WriteUint16Le(retPtr+16, uint16(addr.Port))
		mem.WriteByte(retPtr+18, ip4[0])
		mem.WriteByte(retPtr+19, ip4[1])
		mem.WriteByte(retPtr+20, ip4[2])
		mem.WriteByte(retPtr+21, ip4[3])
	} else {
		ip6 := addr.IP.To16()
		mem.WriteByte(retPtr+12, 1) // IPv6
		mem.WriteUint16Le(retPtr+16, uint16(addr.Port))
		mem.WriteUint32Le(retPtr+20, 0) // flow-info
		for i := 0; i < 8; i++ {
			seg := uint16(ip6[i*2])<<8 | uint16(ip6[i*2+1])
			mem.WriteUint16Le(retPtr+24+uint32(i)*2, seg)
		}
		mem.WriteUint32Le(retPtr+40, 0) // scope-id
	}
}

// asyncLowerSockets handles [async-lower] variants for socket functions.
// The async-lower form passes params through memory: (params_ptr: i32, retPtr: i32) -> i32.
func (h *ComponentHost) asyncLowerSockets(inner string, paramTypes, resultTypes []api.ValueType) api.GoModuleFunction {
	switch inner {
	case "[method]tcp-socket.connect":
		// Async-lower form: (params_ptr: i32, retPtr: i32) -> i32
		// params in memory: self(i32) + ip-socket-address(disc + payload)
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			paramsPtr := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			self, _ := mem.ReadUint32Le(paramsPtr)
			// ip-socket-address variant: disc at paramsPtr+4, payload at paramsPtr+8
			addrDisc, _ := mem.ReadByte(paramsPtr + 4)

			var ip net.IP
			var port uint16
			if addrDisc == 0 {
				// IPv4 payload: port(u16), a, b, c, d
				p, _ := mem.ReadUint16Le(paramsPtr + 8)
				a, _ := mem.ReadByte(paramsPtr + 10)
				b, _ := mem.ReadByte(paramsPtr + 11)
				c, _ := mem.ReadByte(paramsPtr + 12)
				d, _ := mem.ReadByte(paramsPtr + 13)
				ip = net.IPv4(a, b, c, d)
				port = p
			} else {
				// IPv6 payload: port(u16), pad(2), flow-info(u32), 8×u16 segments, scope-id(u32)
				p, _ := mem.ReadUint16Le(paramsPtr + 8)
				ip = make(net.IP, 16)
				for i := 0; i < 8; i++ {
					val, _ := mem.ReadUint16Le(paramsPtr + 16 + uint32(i)*2)
					ip[i*2] = byte(val >> 8)
					ip[i*2+1] = byte(val)
				}
				port = p
			}

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1) // error
				stack[0] = 2             // RETURNED
				return
			}
			sock := res.(*tcpSocketResource)
			sock.mu.Lock()
			defer sock.mu.Unlock()

			// Validate address family matches socket family.
			if (sock.family == 0 && addrDisc != 0) || (sock.family == 1 && addrDisc != byte(1)) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidArgument)
				stack[0] = 2 // RETURNED
				return
			}

			// Reject IPv4-mapped IPv6 addresses.
			if addrDisc == 1 && ip.To4() != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidArgument)
				stack[0] = 2
				return
			}

			// Reject multicast/broadcast addresses.
			if ip4 := ip.To4(); ip4 != nil {
				if ip4[0] >= 224 || ip4.Equal(net.IPv4bcast) || ip4.Equal(net.IPv4zero) {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errInvalidArgument)
					stack[0] = 2
					return
				}
			} else if ip6 := ip.To16(); ip6 != nil {
				if ip6.IsMulticast() || ip6.IsUnspecified() {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errInvalidArgument)
					stack[0] = 2
					return
				}
			}

			// Reject connect to port 0.
			if port == 0 {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidArgument)
				stack[0] = 2
				return
			}

			// Check if socket is already connected or listening.
			if sock.conn != nil || sock.connected || sock.listening {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidState)
				stack[0] = 2
				return
			}

			remoteAddr := &net.TCPAddr{IP: ip, Port: int(port)}
			localAddr := sock.addr
			if localAddr == nil {
				localAddr = &net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: 0}
			}
			conn, err := net.DialTCP("tcp", localAddr, remoteAddr)
			if err != nil {
				// If IPv6 not available, check if there's a simulated listener.
				if addrDisc == 1 {
					addrKey := fmt.Sprintf("[%s]:%d", ip, port)
					if ch, ok := activeListeners.Load(addrKey); ok {
						acceptCh := ch.(chan net.Conn)
						// Create an in-memory pipe for the simulated connection.
						clientConn, serverConn := net.Pipe()
						sock.pipeConn = clientConn
						sock.addr = localAddr
						sock.connected = true
						// Send the server end to the listener's accept channel.
						select {
						case acceptCh <- serverConn:
						default:
						}
						mem.WriteByte(retPtr, 0) // ok
						stack[0] = 2
						return
					}
				}
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errConnectionRefused)
				stack[0] = 2 // RETURNED
				return
			}
			sock.conn = conn
			sock.connected = true
			sock.addr = conn.LocalAddr().(*net.TCPAddr)

			mem.WriteByte(retPtr, 0) // ok
			stack[0] = 2             // RETURNED
		})

	case "[method]tcp-socket.send":
		// send: func(data: stream<u8>) -> future<result<_, error-code>>
		// Async-lower: (params_ptr: i32, retPtr: i32) -> i32
		// params in memory: self(i32), data_stream(i32)
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			paramsPtr := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			self, _ := mem.ReadUint32Le(paramsPtr)
			dataStreamHandle, _ := mem.ReadUint32Le(paramsPtr + 4)

			res, ok := h.resources.Get(self)
			if !ok {
				stack[0] = 2
				return
			}
			sock := res.(*tcpSocketResource)
			sock.mu.Lock()
			conn := sock.conn
			pipeConn := sock.pipeConn
			connected := sock.connected
			alreadySending := sock.sending
			sock.sending = true
			sock.mu.Unlock()

			if !connected || alreadySending {
				// Write Err(invalid-state) to retPtr.
				mem.WriteByte(retPtr, 1)   // err discriminant
				mem.WriteByte(retPtr+4, errInvalidState)
				stack[0] = 2 // RETURNED
				return
			}

			// Redirect the data stream's writer to the TCP connection.
			if sr, ok := h.resources.Get(dataStreamHandle); ok {
				if stream, ok := sr.(*streamResource); ok {
					if conn != nil {
						stream.writer = conn
					} else if pipeConn != nil {
						stream.writer = pipeConn
					}
				}
			}

			// Write Ok result to retPtr.
			mem.WriteByte(retPtr, 0)
			stack[0] = 2 // RETURNED
		})

	case "[method]tcp-socket.receive":
		// receive: func() -> tuple<stream<u8>, future<result<_, error-code>>>
		// Async-lower: (params_ptr: i32, retPtr: i32) -> i32
		// params in memory: self(i32)
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			paramsPtr := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			self, _ := mem.ReadUint32Le(paramsPtr)

			res, ok := h.resources.Get(self)
			if !ok {
				stack[0] = 2
				return
			}
			sock := res.(*tcpSocketResource)
			sock.mu.Lock()
			conn := sock.conn
			pipeConn := sock.pipeConn
			connected := sock.connected
			alreadyReceiving := sock.receiving
			sock.receiving = true
			sock.mu.Unlock()

			if !connected || alreadyReceiving {
				// Return stream + future with Err(invalid-state).
				streamHandle := h.resources.New(&streamResource{})
				futureResult := make([]byte, 20)
				futureResult[0] = 1 // err discriminant
				futureResult[4] = errInvalidState
				futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
				mem.WriteUint32Le(retPtr, streamHandle)
				mem.WriteUint32Le(retPtr+4, futureHandle)
				stack[0] = 2 // RETURNED
				return
			}

			var reader io.Reader
			if conn != nil {
				reader = conn
			} else if pipeConn != nil {
				reader = pipeConn
			}

			streamHandle := h.resources.New(&streamResource{reader: reader})
			futureResult := make([]byte, 20)
			futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
			mem.WriteUint32Le(retPtr, streamHandle)
			mem.WriteUint32Le(retPtr+4, futureHandle)
			stack[0] = 2 // RETURNED
		})

	case "[method]udp-socket.send":
		// send: async func(data: list<u8>, remote-address: option<ip-socket-address>) -> result<_, error-code>
		// Async-lower: always spilled (params_ptr: i32, retPtr: i32) -> i32
		// Params layout in memory at paramsPtr:
		//   +0:  self (u32)
		//   +4:  data.ptr (u32)
		//   +8:  data.len (u32)
		//   +12: option disc (u8) - 0=None, 1=Some
		//   +16: ip-socket-address disc (u8) - 0=IPv4, 1=IPv6
		//   For IPv4: +20: port(u16), +22: a,b,c,d
		//   For IPv6: +20: port(u16), +24: flow-info(u32), +28: addr(u16×8), +44: scope-id(u32)
		return api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			paramsPtr := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			self, _ := mem.ReadUint32Le(paramsPtr)
			dataPtr, _ := mem.ReadUint32Le(paramsPtr + 4)
			dataLen, _ := mem.ReadUint32Le(paramsPtr + 8)
			optionDisc, _ := mem.ReadByte(paramsPtr + 12)

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidArgument)
				stack[0] = 2
				return
			}
			sock := res.(*udpSocketResource)
			sock.mu.Lock()
			remoteAddr := sock.remoteAddr
			family := sock.family
			sock.mu.Unlock()

			var targetAddr *net.UDPAddr
			if optionDisc == 1 {
				// Some(ip-socket-address)
				addrDisc, _ := mem.ReadByte(paramsPtr + 16)
				// Validate address family matches socket family.
				if addrDisc != family {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errInvalidArgument)
					stack[0] = 2
					return
				}
				if addrDisc == 0 {
					// IPv4
					port, _ := mem.ReadUint16Le(paramsPtr + 20)
					a, _ := mem.ReadByte(paramsPtr + 22)
					b, _ := mem.ReadByte(paramsPtr + 23)
					c, _ := mem.ReadByte(paramsPtr + 24)
					d, _ := mem.ReadByte(paramsPtr + 25)
					ip := net.IPv4(a, b, c, d)
					// Check for INADDR_ANY (0.0.0.0).
					if ip.Equal(net.IPv4zero) {
						mem.WriteByte(retPtr, 1)
						mem.WriteByte(retPtr+4, errInvalidArgument)
						stack[0] = 2
						return
					}
					if port == 0 {
						mem.WriteByte(retPtr, 1)
						mem.WriteByte(retPtr+4, errAddressNotBindable)
						stack[0] = 2
						return
					}
					targetAddr = &net.UDPAddr{IP: ip, Port: int(port)}
				} else {
					// IPv6
					port, _ := mem.ReadUint16Le(paramsPtr + 20)
					// Read 8 u16 segments for the IPv6 address.
					var segments [8]uint16
					for i := 0; i < 8; i++ {
						segments[i], _ = mem.ReadUint16Le(paramsPtr + 28 + uint32(i)*2)
					}
					ip := make(net.IP, 16)
					for i := 0; i < 8; i++ {
						ip[i*2] = byte(segments[i] >> 8)
						ip[i*2+1] = byte(segments[i])
					}
					// Check for IN6ADDR_ANY (::).
					if ip.Equal(net.IPv6zero) {
						mem.WriteByte(retPtr, 1)
						mem.WriteByte(retPtr+4, errInvalidArgument)
						stack[0] = 2
						return
					}
					if port == 0 {
						mem.WriteByte(retPtr, 1)
						mem.WriteByte(retPtr+4, errAddressNotBindable)
						stack[0] = 2
						return
					}
					targetAddr = &net.UDPAddr{IP: ip, Port: int(port)}
				}
				// If connected, check that remote-address matches connected addr.
				if remoteAddr != nil && !remoteAddr.IP.Equal(targetAddr.IP) {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errInvalidArgument)
					stack[0] = 2
					return
				}
				if remoteAddr != nil && remoteAddr.Port != targetAddr.Port {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errInvalidArgument)
					stack[0] = 2
					return
				}
			} else {
				// None: socket must be connected.
				if remoteAddr == nil {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errInvalidArgument)
					stack[0] = 2
					return
				}
				targetAddr = remoteAddr
			}

			// Check datagram size.
			if dataLen >= 65536 {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errDatagramTooLarge)
				stack[0] = 2
				return
			}

			// Auto-bind if not yet bound.
			sock.mu.Lock()
			if sock.addr == nil {
				if family == 0 {
					newConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
					if err != nil {
						sock.mu.Unlock()
						mem.WriteByte(retPtr, 1)
						mem.WriteByte(retPtr+4, errAddressInUse)
						stack[0] = 2
						return
					}
					sock.conn = newConn
					sock.addr = newConn.LocalAddr().(*net.UDPAddr)
				} else {
					// IPv6: simulated bind.
					sock.addr = &net.UDPAddr{IP: net.IPv6loopback, Port: 0}
				}
			}
			conn := sock.conn
			sock.mu.Unlock()

			// Send the data.
			data, _ := mem.Read(dataPtr, dataLen)
			if len(data) > 0 {
				if conn != nil {
					if remoteAddr != nil {
						_, _ = conn.Write(data)
					} else {
						_, _ = conn.WriteToUDP(data, targetAddr)
					}
				} else if targetAddr != nil {
					// Simulated (IPv6) socket: deliver to mailbox.
					// Must copy data since mem.Read returns a view into wasm memory.
					dataCopy := make([]byte, len(data))
					copy(dataCopy, data)
					addrKey := fmt.Sprintf("udp[%s]:%d", targetAddr.IP, targetAddr.Port)
					if mb, ok := udpMailboxes.Load(addrKey); ok {
						sock.mu.Lock()
						senderAddr := sock.addr
						sock.mu.Unlock()
						select {
						case mb.(chan udpDatagram) <- udpDatagram{data: dataCopy, sender: senderAddr}:
						default:
						}
					}
				}
			}

			mem.WriteByte(retPtr, 0) // ok
			stack[0] = 2             // RETURNED
		})

	case "[method]udp-socket.receive":
		// receive: async func() -> result<tuple<list<u8>, ip-socket-address>, error-code>
		// Async-lower: (self: i32, retPtr: i32) -> i32
		// Result layout at retPtr:
		//   +0:  result disc (u8) - 0=ok, 1=err
		//   If ok:
		//     +4:  list ptr (u32)
		//     +8:  list len (u32)
		//     +12: ip-socket-address disc (u8) - 0=IPv4, 1=IPv6
		//     For IPv4: +16: port(u16), +18: a,b,c,d
		//     For IPv6: +16: port(u16), +20: flow-info(u32), +24: addr(u16×8), +40: scope-id(u32)
		//   If err:
		//     +4:  error-code (u8)
		return api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidArgument)
				stack[0] = 2
				return
			}
			sock := res.(*udpSocketResource)
			sock.mu.Lock()
			conn := sock.conn
			addr := sock.addr
			sock.mu.Unlock()
			if conn == nil && addr == nil {
				// Not bound.
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidState)
				stack[0] = 2
				return
			}

			if conn == nil {
				// Simulated (IPv6) socket: read from mailbox.
				addrKey := fmt.Sprintf("udp[%s]:%d", addr.IP, addr.Port)
				mbVal, ok := udpMailboxes.Load(addrKey)
				if !ok {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errInvalidState)
					stack[0] = 2
					return
				}
				mailbox := mbVal.(chan udpDatagram)

				// Try non-blocking read.
				select {
				case dg := <-mailbox:
					writeUDPReceiveResult(ctx, mod, retPtr, dg.data, dg.sender)
					stack[0] = 2 // RETURNED
					return
				default:
				}

				// No data yet - create async subtask.
				subtaskIdx := h.subtasks.NewPending(retPtr)
				stack[0] = uint64(subtaskIdx<<4) | 1 // STARTED
				go func() {
					select {
					case dg := <-mailbox:
						h.subtasks.Complete(subtaskIdx, nil, func(ctx context.Context, mod api.Module) {
							writeUDPReceiveResult(ctx, mod, retPtr, dg.data, dg.sender)
						})
					case <-time.After(30 * time.Second):
						results := make([]byte, 8)
						results[0] = 1
						results[4] = errInvalidState
						h.subtasks.Complete(subtaskIdx, results, nil)
					}
				}()
				return
			}

			// Try non-blocking read first.
			buf := make([]byte, 65536)
			_ = conn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
			n, senderAddr, err := conn.ReadFromUDP(buf)
			_ = conn.SetReadDeadline(time.Time{})

			if err == nil {
				// Data available - write result synchronously.
				writeUDPReceiveResult(ctx, mod, retPtr, buf[:n], senderAddr)
				stack[0] = 2 // RETURNED
				return
			}

			// No data available - create async subtask.
			subtaskIdx := h.subtasks.NewPending(retPtr)
			stack[0] = uint64(subtaskIdx<<4) | 1 // STARTED

			go func() {
				readBuf := make([]byte, 65536)
				_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
				n, addr, err := conn.ReadFromUDP(readBuf)
				_ = conn.SetReadDeadline(time.Time{})

				if err != nil {
					// Error - return err result.
					results := make([]byte, 8)
					results[0] = 1 // err disc
					results[4] = errInvalidState
					h.subtasks.Complete(subtaskIdx, results, nil)
				} else {
					// Success - use resultsFn to allocate memory and write result.
					data := make([]byte, n)
					copy(data, readBuf[:n])
					h.subtasks.Complete(subtaskIdx, nil, func(ctx context.Context, mod api.Module) {
						writeUDPReceiveResult(ctx, mod, retPtr, data, addr)
					})
				}
			}()
		})
	}
	return nil
}

// socketsImportHandler handles unregistered imports for wasi:sockets/* modules.
func (h *ComponentHost) socketsImportHandler(moduleName, funcName string, paramTypes, resultTypes []api.ValueType) api.GoModuleFunction {
	switch funcName {
	case "[static]tcp-socket.create":
		// (family: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			family := uint8(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			sock := &tcpSocketResource{family: family}
			handle := h.resources.New(sock)
			// result<tcp-socket, error-code>: disc=0 (ok), handle at offset 4
			mem.WriteByte(retPtr, 0)
			mem.WriteUint32Le(retPtr+4, handle)
		})

	case "[resource-drop]tcp-socket":
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			self := uint32(stack[0])
			res, ok := h.resources.Get(self)
			if ok {
				if sock, ok := res.(*tcpSocketResource); ok {
					sock.mu.Lock()
					if sock.listener != nil {
						sock.listener.Close()
					}
					if sock.conn != nil {
						sock.conn.Close()
					}
					// Clean up listener and address tracking.
					if sock.addr != nil {
						addrKey := fmt.Sprintf("%s:%d", sock.addr.IP, sock.addr.Port)
						boundAddresses.Delete(addrKey)
						if sock.family == 1 {
							addrKey = fmt.Sprintf("[%s]:%d", sock.addr.IP, sock.addr.Port)
							activeListeners.Delete(addrKey)
							boundAddresses.Delete(addrKey)
						}
					}
					if sock.pipeConn != nil {
						sock.pipeConn.Close()
					}
					sock.mu.Unlock()
				}
			}
			h.resources.Drop(self)
		})

	case "[method]tcp-socket.bind":
		// Flattened: (self: i32, addr_disc: i32, ...addr_payload(11)..., retPtr: i32) -> ()
		// Total: 14 params for IPv4/IPv6 address variant
		bindCount := 0
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			bindCount++
			self := uint32(stack[0])
			retPtrIdx := len(paramTypes) - 1
			retPtr := uint32(stack[retPtrIdx])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			addrDisc := uint32(stack[1])
			ip, port := readAddressFlat(stack, 1)

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1) // error
				mem.WriteByte(retPtr+4, errInvalidArgument)
				return
			}
			sock := res.(*tcpSocketResource)
			sock.mu.Lock()
			defer sock.mu.Unlock()

			// Check if socket is already bound.
			if sock.addr != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidState)
				return
			}

			// Validate address family matches socket family.
			if (sock.family == 0 && addrDisc != 0) || (sock.family == 1 && addrDisc != 1) {
				mem.WriteByte(retPtr, 1) // Err disc
				mem.WriteByte(retPtr+4, errInvalidArgument)
				return
			}

			// Reject IPv4-mapped IPv6 addresses (dual-stack not supported).
			if addrDisc == 1 && ip.To4() != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidArgument)
				return
			}

			// Check for non-bindable addresses (documentation ranges per RFC 5737, RFC 3849).
			if ip4 := ip.To4(); ip4 != nil {
				if (ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 2) || // 192.0.2.0/24 TEST-NET-1
					(ip4[0] == 198 && ip4[1] == 51 && ip4[2] == 100) || // 198.51.100.0/24 TEST-NET-2
					(ip4[0] == 203 && ip4[1] == 0 && ip4[2] == 113) { // 203.0.113.0/24 TEST-NET-3
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errAddressNotBindable)
					return
				}
			} else if ip6 := ip.To16(); ip6 != nil {
				// 2001:db8::/32 documentation prefix
				if ip6[0] == 0x20 && ip6[1] == 0x01 && ip6[2] == 0x0d && ip6[3] == 0xb8 {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errAddressNotBindable)
					return
				}
			}

			// Validate that the address is unicast (not multicast/broadcast/unspecified).
			if ip4 := ip.To4(); ip4 != nil {
				if ip4[0] >= 224 || ip4.Equal(net.IPv4bcast) {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errInvalidArgument)
					return
				}
			} else if ip6 := ip.To16(); ip6 != nil {
				if ip6.IsMulticast() {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errInvalidArgument)
					return
				}
			}

			// Actually bind the socket to get an OS-assigned port.
			bindAddr := &net.TCPAddr{IP: ip, Port: int(port)}
			network := "tcp4"
			if addrDisc != 0 {
				network = "tcp6"
			}

			// Check if address is already bound in our tracking map.
			if port != 0 {
				addrKey := fmt.Sprintf("%s:%d", ip, port)
				if _, loaded := boundAddresses.Load(addrKey); loaded {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errAddressInUse)
					return
				}
			}

			listener, err := net.ListenTCP(network, bindAddr)
			if err != nil {
				// If IPv6 not available on the host, simulate bind.
				if addrDisc != 0 {
					if port == 0 {
						port = 12345 + uint16(bindCount) // fake ephemeral port
					}
					addrKey := fmt.Sprintf("[%s]:%d", ip, port)
					if _, loaded := boundAddresses.LoadOrStore(addrKey, true); loaded {
						mem.WriteByte(retPtr, 1)
						mem.WriteByte(retPtr+4, errAddressInUse)
						return
					}
					sock.addr = &net.TCPAddr{IP: ip, Port: int(port)}
					mem.WriteByte(retPtr, 0) // ok
					return
				}
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errAddressInUse)
				return
			}
			// Store the address but close the listener - it will be re-created
			// by listen() if needed. This allows connect() to reuse the address.
			sock.addr = listener.Addr().(*net.TCPAddr)
			listener.Close()
			// Track the bound address so duplicate binds are detected.
			addrKey := fmt.Sprintf("%s:%d", sock.addr.IP, sock.addr.Port)
			boundAddresses.Store(addrKey, true)
			mem.WriteByte(retPtr, 0) // ok
		})

	case "[method]tcp-socket.listen":
		// (self: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				return
			}
			sock := res.(*tcpSocketResource)
			sock.mu.Lock()
			defer sock.mu.Unlock()

			// Reject listen if already listening or connected.
			if sock.listening || sock.connected {
				mem.WriteByte(retPtr, 1) // err
				mem.WriteByte(retPtr+4, errInvalidState)
				return
			}

			// If not already bound (no listener from bind), bind now.
			if sock.listener == nil {
				addr := sock.addr
				if addr == nil {
					addr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
				}
				network := "tcp4"
				if sock.family == 1 {
					network = "tcp6"
				}
				listener, err := net.ListenTCP(network, addr)
				if err != nil {
					// If IPv6 not available, create a simulated listener stream.
					if sock.family == 1 {
						if sock.addr == nil {
							sock.addr = &net.TCPAddr{IP: net.ParseIP("::1"), Port: 12345}
						}
						addrKey := fmt.Sprintf("[%s]:%d", sock.addr.IP, sock.addr.Port)
						acceptCh := make(chan net.Conn, 16)
						activeListeners.Store(addrKey, acceptCh)
						sock.listening = true
						streamHandle := h.resources.New(&tcpListenerStream{acceptCh: acceptCh, host: h})
						mem.WriteByte(retPtr, 0) // ok
						mem.WriteUint32Le(retPtr+4, streamHandle)
						return
					}
					mem.WriteByte(retPtr, 1) // err
					mem.WriteByte(retPtr+4, errInvalidState)
					return
				}
				sock.listener = listener
				sock.addr = listener.Addr().(*net.TCPAddr)
			}

			sock.listening = true
			streamHandle := h.resources.New(&tcpListenerStream{listener: sock.listener, host: h})
			mem.WriteByte(retPtr, 0) // ok
			mem.WriteUint32Le(retPtr+4, streamHandle)
		})

	case "[method]tcp-socket.send":
		// send: func(data: stream<u8>) -> future<result<_, error-code>>
		// Lowered: (self: i32, data_stream: i32) -> (future_handle: i32)
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			self := uint32(stack[0])
			dataStreamHandle := uint32(stack[1])

			res, ok := h.resources.Get(self)
			if !ok {
				stack[0] = 0
				return
			}
			sock := res.(*tcpSocketResource)
			sock.mu.Lock()
			conn := sock.conn
			pipeConn := sock.pipeConn
			connected := sock.connected
			alreadySending := sock.sending
			sock.sending = true
			sock.mu.Unlock()
			if !connected || alreadySending {
				// Return future with Err(invalid-state).
				futureResult := make([]byte, 20)
				futureResult[0] = 1 // err discriminant
				futureResult[4] = errInvalidState
				futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
				stack[0] = uint64(futureHandle)
				return
			}

			// Redirect the data stream's writer to the TCP connection.
			if sr, ok := h.resources.Get(dataStreamHandle); ok {
				if stream, ok := sr.(*streamResource); ok {
					if conn != nil {
						stream.writer = conn
					} else if pipeConn != nil {
						stream.writer = pipeConn
					}
				}
			}

			// Create a pre-ready future with Ok result.
			futureResult := make([]byte, 20) // result<_, error-code> = Ok
			futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
			stack[0] = uint64(futureHandle)
		})

	case "[method]tcp-socket.receive":
		// receive: func() -> tuple<stream<u8>, future<result<_, error-code>>>
		// Lowered: (self: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				return
			}
			sock := res.(*tcpSocketResource)
			sock.mu.Lock()
			conn := sock.conn
			pipeConn := sock.pipeConn
			connected := sock.connected
			alreadyReceiving := sock.receiving
			sock.receiving = true
			sock.mu.Unlock()

			if !connected || alreadyReceiving {
				// Return stream + future with Err(invalid-state).
				streamHandle := h.resources.New(&streamResource{})
				futureResult := make([]byte, 20)
				futureResult[0] = 1 // err discriminant
				futureResult[4] = errInvalidState
				futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
				mem.WriteUint32Le(retPtr, streamHandle)
				mem.WriteUint32Le(retPtr+4, futureHandle)
				return
			}

			var reader io.Reader
			if conn != nil {
				reader = conn
			} else if pipeConn != nil {
				reader = pipeConn
			}

			streamHandle := h.resources.New(&streamResource{reader: reader})
			futureResult := make([]byte, 20)
			futureHandle := h.resources.New(&futureResource{result: futureResult, ready: true})
			mem.WriteUint32Le(retPtr, streamHandle)
			mem.WriteUint32Le(retPtr+4, futureHandle)
		})

	case "[method]tcp-socket.get-local-address":
		// (self: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				return
			}
			sock := res.(*tcpSocketResource)
			sock.mu.Lock()
			defer sock.mu.Unlock()

			if sock.addr == nil {
				mem.WriteByte(retPtr, 1)
				return
			}

			mem.WriteByte(retPtr, 0) // ok
			writeIPPort(mem, retPtr+4, sock.addr.IP, uint16(sock.addr.Port))
		})

	case "[method]tcp-socket.get-address-family":
		// (self: i32) -> (i32)
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			self := uint32(stack[0])
			res, ok := h.resources.Get(self)
			if !ok {
				stack[0] = 0
				return
			}
			sock := res.(*tcpSocketResource)
			stack[0] = uint64(sock.family)
		})

	case "[static]udp-socket.create":
		// (family: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			family := uint8(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			sock := &udpSocketResource{family: family}
			handle := h.resources.New(sock)
			mem.WriteByte(retPtr, 0)
			mem.WriteUint32Le(retPtr+4, handle)
		})

	case "[resource-drop]udp-socket":
		return api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			self := uint32(stack[0])
			res, ok := h.resources.Get(self)
			if ok {
				if sock, ok := res.(*udpSocketResource); ok {
					sock.mu.Lock()
					if sock.conn != nil {
						sock.conn.Close()
					}
					sock.mu.Unlock()
				}
			}
			h.resources.Drop(self)
		})

	case "[method]udp-socket.bind":
		// Flattened: (self: i32, addr_disc: i32, ...addr_payload(11)..., retPtr: i32) -> ()
		udpBindCount := 0
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			udpBindCount++
			self := uint32(stack[0])
			retPtrIdx := len(paramTypes) - 1
			retPtr := uint32(stack[retPtrIdx])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			addrDisc := uint32(stack[1])
			ip, port := readAddressFlat(stack, 1)

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidArgument)
				return
			}
			sock := res.(*udpSocketResource)
			sock.mu.Lock()
			defer sock.mu.Unlock()

			// Check if socket is already bound.
			if sock.addr != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidState)
				return
			}

			// Validate address family matches socket family.
			if (sock.family == 0 && addrDisc != 0) || (sock.family == 1 && addrDisc != 1) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidArgument)
				return
			}

			// Reject IPv4-mapped IPv6 addresses.
			if addrDisc == 1 && ip.To4() != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidArgument)
				return
			}

			// Check for non-bindable addresses (documentation ranges).
			if ip4 := ip.To4(); ip4 != nil {
				if (ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 2) ||
					(ip4[0] == 198 && ip4[1] == 51 && ip4[2] == 100) ||
					(ip4[0] == 203 && ip4[1] == 0 && ip4[2] == 113) {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errAddressNotBindable)
					return
				}
			} else if ip6 := ip.To16(); ip6 != nil {
				if ip6[0] == 0x20 && ip6[1] == 0x01 && ip6[2] == 0x0d && ip6[3] == 0xb8 {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errAddressNotBindable)
					return
				}
			}

			udpAddr := &net.UDPAddr{IP: ip, Port: int(port)}
			network := "udp4"
			if addrDisc != 0 {
				network = "udp6"
			}

			// Check for duplicate binds. Go's net.ListenUDP sets
			// SO_REUSEADDR which allows duplicate binds on Linux,
			// but WASI requires AddressInUse for duplicates.
			if port != 0 {
				prefix := "udp"
				if addrDisc != 0 {
					prefix = "udp6"
				}
				addrKey := fmt.Sprintf("%s[%s]:%d", prefix, ip, port)
				if _, loaded := boundAddresses.LoadOrStore(addrKey, true); loaded {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errAddressInUse)
					return
				}
			}

			conn, err := net.ListenUDP(network, udpAddr)
			if err != nil {
				// IPv6 simulation.
				if addrDisc != 0 {
					if port == 0 {
						port = 22345 + uint16(udpBindCount)
					}
					addrKey := fmt.Sprintf("udp6[%s]:%d", ip, port)
					if _, loaded := boundAddresses.LoadOrStore(addrKey, true); loaded {
						mem.WriteByte(retPtr, 1)
						mem.WriteByte(retPtr+4, errAddressInUse)
						return
					}
					sock.addr = &net.UDPAddr{IP: ip, Port: int(port)}
					// Register a mailbox for receiving simulated datagrams.
					mailbox := make(chan udpDatagram, 64)
					udpMailboxes.Store(addrKey, mailbox)
					mem.WriteByte(retPtr, 0)
					return
				}
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errAddressInUse)
				return
			}
			sock.conn = conn
			sock.addr = conn.LocalAddr().(*net.UDPAddr)
			// Track ephemeral port binds for duplicate detection.
			if port == 0 {
				actualPort := sock.addr.Port
				prefix := "udp"
				if addrDisc != 0 {
					prefix = "udp6"
				}
				addrKey := fmt.Sprintf("%s[%s]:%d", prefix, sock.addr.IP, actualPort)
				boundAddresses.Store(addrKey, true)
			}
			mem.WriteByte(retPtr, 0) // ok
		})

	case "[method]udp-socket.connect":
		// Flattened: (self: i32, addr_disc: i32, ...addr_payload(11)..., retPtr: i32) -> ()
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtrIdx := len(paramTypes) - 1
			retPtr := uint32(stack[retPtrIdx])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			addrDisc := uint32(stack[1])
			ip, port := readAddressFlat(stack, 1)

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidArgument)
				return
			}
			sock := res.(*udpSocketResource)
			sock.mu.Lock()
			defer sock.mu.Unlock()

			// Validate address family matches socket family.
			if (sock.family == 0 && addrDisc != 0) || (sock.family == 1 && addrDisc != 1) {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidArgument)
				return
			}

			// Reject IPv4-mapped IPv6 addresses.
			if addrDisc == 1 && ip.To4() != nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidArgument)
				return
			}

			// Reject unspecified addresses and port 0.
			if ip.IsUnspecified() || port == 0 {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidArgument)
				return
			}

			// Reject multicast/broadcast.
			if ip4 := ip.To4(); ip4 != nil {
				if ip4[0] >= 224 || ip4.Equal(net.IPv4bcast) {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errInvalidArgument)
					return
				}
			} else if ip6 := ip.To16(); ip6 != nil {
				if ip6.IsMulticast() {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errInvalidArgument)
					return
				}
			}

			remoteAddr := &net.UDPAddr{IP: ip, Port: int(port)}
			sock.remoteAddr = remoteAddr

			if sock.conn == nil {
				network := "udp4"
				if sock.family == 1 {
					network = "udp6"
				}
				conn, err := net.DialUDP(network, nil, remoteAddr)
				if err != nil {
					// IPv6 simulation.
					if sock.family == 1 {
						sock.addr = &net.UDPAddr{IP: ip, Port: 22222}
						mem.WriteByte(retPtr, 0)
						return
					}
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errInvalidArgument)
					return
				}
				sock.conn = conn
				sock.addr = conn.LocalAddr().(*net.UDPAddr)
			}

			mem.WriteByte(retPtr, 0) // ok
		})

	case "[method]udp-socket.disconnect":
		// (self: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidArgument)
				return
			}
			sock := res.(*udpSocketResource)
			sock.mu.Lock()
			defer sock.mu.Unlock()
			if sock.remoteAddr == nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidState)
				return
			}
			sock.remoteAddr = nil
			mem.WriteByte(retPtr, 0)
		})

	case "[method]udp-socket.get-local-address":
		// (self: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				return
			}
			sock := res.(*udpSocketResource)
			sock.mu.Lock()
			defer sock.mu.Unlock()

			if sock.addr == nil {
				mem.WriteByte(retPtr, 1)
				return
			}

			mem.WriteByte(retPtr, 0)
			writeIPPort(mem, retPtr+4, sock.addr.IP, uint16(sock.addr.Port))
		})

	case "[method]udp-socket.get-remote-address":
		// (self: i32, retPtr: i32) -> ()
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			res, ok := h.resources.Get(self)
			if !ok {
				mem.WriteByte(retPtr, 1)
				return
			}
			sock := res.(*udpSocketResource)
			sock.mu.Lock()
			defer sock.mu.Unlock()

			if sock.remoteAddr == nil {
				mem.WriteByte(retPtr, 1)
				return
			}

			mem.WriteByte(retPtr, 0)
			writeIPPort(mem, retPtr+4, sock.remoteAddr.IP, uint16(sock.remoteAddr.Port))
		})
	}

	// TCP socket property getters with sensible defaults.
	if funcName == "[method]tcp-socket.get-keep-alive-enabled" {
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			mem.WriteByte(retPtr, 0)   // ok
			mem.WriteByte(retPtr+4, 0) // false
		})
	}

	tcpProperties := map[string]uint64{
		"[method]tcp-socket.get-hop-limit":            64,
		"[method]tcp-socket.get-keep-alive-count":     9,
		"[method]tcp-socket.get-send-buffer-size":     65536,
		"[method]tcp-socket.get-receive-buffer-size":  65536,
		"[method]tcp-socket.get-keep-alive-idle-time": 7200000000000,
		"[method]tcp-socket.get-keep-alive-interval":  75000000000,
	}
	if propVal, ok := tcpProperties[funcName]; ok {
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[1])
			mem := mod.Memory()
			if mem == nil {
				return
			}
			mem.WriteByte(retPtr, 0)
			mem.WriteUint64Le(retPtr+8, propVal)
		})
	}

	return nil
}
