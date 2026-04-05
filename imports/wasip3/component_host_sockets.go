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

// tcpConnectionResource wraps an accepted TCP connection.
type tcpConnectionResource struct {
	conn *net.TCPConn
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
)

// registerSockets is a no-op; all socket functions are handled dynamically by
// socketsImportHandler to avoid signature mismatches with the flat ABI lowering.
func (h *ComponentHost) registerSockets(cl *wazero.ComponentLinker) {}

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
		// send: func(data: list<datagram>) -> result<_, error-code>
		// Async-lower: params depend on how many fit flat.
		// If len(paramTypes) == 2: spilled (params_ptr, retPtr)
		// If len(paramTypes) > 2: direct (self, list_ptr, list_len, retPtr)
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			retPtr := uint32(stack[len(paramTypes)-1])
			mem := mod.Memory()
			if mem == nil {
				return
			}

			var self uint32
			var paramsPtr uint32
			if len(paramTypes) == 2 {
				// Spilled: params in memory.
				paramsPtr = uint32(stack[0])
				self, _ = mem.ReadUint32Le(paramsPtr)
			} else {
				// Direct params on stack.
				self = uint32(stack[0])
			}
			// list<datagram>: ptr at paramsPtr+4, len at paramsPtr+8
			// Each datagram has: data(ptr, len) + remote-address(optional variant)

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
			remoteAddr := sock.remoteAddr
			sock.mu.Unlock()

			if conn == nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidState)
				stack[0] = 2
				return
			}

			// Read the datagram list.
			var listPtr, listLen uint32
			if len(paramTypes) == 2 {
				listPtr, _ = mem.ReadUint32Le(paramsPtr + 4)
				listLen, _ = mem.ReadUint32Le(paramsPtr + 8)
			} else {
				listPtr = uint32(stack[1])
				listLen = uint32(stack[2])
			}

			// Each datagram struct in memory:
			// data_ptr(4) + data_len(4) + remote_address(ip-socket-address variant)
			// ip-socket-address: disc(1 byte, padded to 4) + payload
			// ipv4 payload: port(u16) + pad(2) + addr(4) = 8 bytes
			// ipv6 payload: port(u16) + pad(2) + flow-info(u32) + addr(16) + scope-id(u32) = 28 bytes
			const datagramSize = 36 // data_ptr(4) + data_len(4) + addr_disc(4) + ipv6_payload(24 padded)
			for i := uint32(0); i < listLen; i++ {
				entryBase := listPtr + i*datagramSize
				dataPtr, _ := mem.ReadUint32Le(entryBase)
				dataLen, _ := mem.ReadUint32Le(entryBase + 4)

				// Check remote address family (disc byte at offset 8).
				addrDisc, _ := mem.ReadByte(entryBase + 8)
				expectedDisc := byte(sock.family)
				if addrDisc != expectedDisc {
					mem.WriteByte(retPtr, 1)
					mem.WriteByte(retPtr+4, errInvalidArgument)
					stack[0] = 2
					return
				}

				data, _ := mem.Read(dataPtr, dataLen)
				if len(data) > 0 {
					if remoteAddr != nil {
						conn.WriteToUDP(data, remoteAddr)
					} else {
						conn.Write(data)
					}
				}
			}

			mem.WriteByte(retPtr, 0) // ok
			stack[0] = 2             // RETURNED
		})

	case "[method]udp-socket.receive":
		// receive: func() -> result<list<datagram>, error-code>
		// Async-lower: (self: i32, retPtr: i32) -> i32
		// Only param is self, so it fits directly on the stack.
		return api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
			self := uint32(stack[0])
			retPtr := uint32(stack[len(paramTypes)-1])
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
			sock.mu.Unlock()

			if conn == nil {
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errInvalidState)
				stack[0] = 2
				return
			}

			// Read up to one datagram.
			buf := make([]byte, 65536)
			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, _, err := conn.ReadFromUDP(buf)
			conn.SetReadDeadline(time.Time{})
			if err != nil {
				// Timeout = no data available.
				mem.WriteByte(retPtr, 0) // ok - empty list
				mem.WriteUint32Le(retPtr+4, 0)
				mem.WriteUint32Le(retPtr+8, 0)
				stack[0] = 2
				return
			}

			// Allocate memory for the datagram data.
			// For simplicity, write result as ok with empty list for now.
			_ = buf[:n]
			mem.WriteByte(retPtr, 0) // ok
			mem.WriteUint32Le(retPtr+4, 0)
			mem.WriteUint32Le(retPtr+8, 0)
			stack[0] = 2
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
			conn, err := net.ListenUDP(network, udpAddr)
			if err != nil {
				// IPv6 simulation.
				if addrDisc != 0 {
					if port == 0 {
						port = 22345 + uint16(udpBindCount)
					}
					addrKey := fmt.Sprintf("udp[%s]:%d", ip, port)
					if _, loaded := boundAddresses.LoadOrStore(addrKey, true); loaded {
						mem.WriteByte(retPtr, 1)
						mem.WriteByte(retPtr+4, errAddressInUse)
						return
					}
					sock.addr = &net.UDPAddr{IP: ip, Port: int(port)}
					mem.WriteByte(retPtr, 0)
					return
				}
				mem.WriteByte(retPtr, 1)
				mem.WriteByte(retPtr+4, errAddressInUse)
				return
			}
			sock.conn = conn
			sock.addr = conn.LocalAddr().(*net.UDPAddr)
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
