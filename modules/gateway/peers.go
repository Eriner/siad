package gateway

import (
	"errors"
	"fmt"
	"net"
	"time"

	"gitlab.com/NebulousLabs/fastrand"
	connmonitor "gitlab.com/NebulousLabs/monitor"
	"gitlab.com/NebulousLabs/ratelimit"

	"gitlab.com/NebulousLabs/encoding"
	"go.sia.tech/siad/build"
	"go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

var (
	errPeerExists       = errors.New("already connected to this peer")
	errPeerRejectedConn = errors.New("peer rejected connection")
)

// insufficientVersionError indicates a peer's version is insufficient.
type insufficientVersionError string

// Error implements the error interface for insufficientVersionError.
func (s insufficientVersionError) Error() string {
	return "unacceptable version: " + string(s)
}

// invalidVersionError indicates a peer's version is not a valid version number.
type invalidVersionError string

// Error implements the error interface for invalidVersionError.
func (s invalidVersionError) Error() string {
	return "invalid version: " + string(s)
}

type peer struct {
	modules.Peer
	m    *connmonitor.Monitor
	rl   *ratelimit.RateLimit
	sess streamSession
}

// sessionHeader is sent after the initial version exchange. It prevents peers
// on different blockchains from connecting to each other, and prevents the
// gateway from connecting to itself.
type sessionHeader struct {
	GenesisID  types.BlockID
	UniqueID   gatewayID
	NetAddress modules.NetAddress
}

func (p *peer) open() (modules.PeerConn, error) {
	conn, err := p.sess.Open()
	if err != nil {
		return nil, err
	}
	// Apply the local ratelimit.
	conn = ratelimit.NewRLConn(conn, p.rl, nil)
	// Apply the global ratelimit.
	conn = ratelimit.NewRLConn(conn, modules.GlobalRateLimits, nil)
	return &peerConn{conn, p.NetAddress}, nil
}

func (p *peer) accept() (modules.PeerConn, error) {
	conn, err := p.sess.Accept()
	if err != nil {
		return nil, err
	}
	return &peerConn{conn, p.NetAddress}, nil
}

// addPeer adds a peer to the Gateway's peer list, spawns a listener thread to
// handle its requests and increments the remotePeers accordingly
func (g *Gateway) addPeer(p *peer) {
	g.peers[p.NetAddress] = p
	go g.threadedListenPeer(p)
}

// callInitRPCs calls the rpcs that are registered to be called upon connecting
// to a peer.
func (g *Gateway) callInitRPCs(addr modules.NetAddress) {
	for name, fn := range g.initRPCs {
		go func(name string, fn modules.RPCFunc) {
			if g.threads.Add() != nil {
				return
			}
			defer g.threads.Done()

			err := g.managedRPC(addr, name, fn)
			if err != nil {
				g.log.Debugf("INFO: RPC %q on peer %q failed: %v", name, addr, err)
			}
		}(name, fn)
	}
}

// randomOutboundPeer returns a random outbound peer.
func (g *Gateway) randomOutboundPeer() (modules.NetAddress, error) {
	// Get the list of outbound peers.
	var addrs []modules.NetAddress
	for addr, peer := range g.peers {
		if peer.Inbound {
			continue
		}
		addrs = append(addrs, addr)
	}
	if len(addrs) == 0 {
		return "", errNoPeers
	}

	// Of the remaining options, select one at random.
	return addrs[fastrand.Intn(len(addrs))], nil
}

// permanentListen handles incoming connection requests. If the connection is
// accepted, the peer will be added to the Gateway's peer list.
func (g *Gateway) permanentListen(closeChan chan struct{}) {
	// Signal that the permanentListen thread has completed upon returning.
	defer close(closeChan)

	for {
		conn, err := g.listener.Accept()
		if err != nil {
			g.log.Debugln("[PL] Closing permanentListen:", err)
			return
		}
		// Monitor bandwidth on conn
		conn = connmonitor.NewMonitoredConn(conn, g.m)

		go g.threadedAcceptConn(conn)

		// Sleep after each accept. This limits the rate at which the Gateway
		// will accept new connections. The intent here is to prevent new
		// incoming connections from kicking out old ones before they have a
		// chance to request additional nodes.
		select {
		case <-time.After(acceptInterval):
		case <-g.threads.StopChan():
			return
		}
	}
}

// threadedAcceptConn adds a connecting node as a peer.
func (g *Gateway) threadedAcceptConn(conn net.Conn) {
	if g.threads.Add() != nil {
		conn.Close()
		return
	}
	defer g.threads.Done()
	conn.SetDeadline(time.Now().Add(connStdDeadline))

	addr := modules.NetAddress(conn.RemoteAddr().String())
	g.log.Debugf("INFO: %v wants to connect", addr)

	g.mu.RLock()
	_, exists := g.blocklist[addr.Host()]
	g.mu.RUnlock()
	if exists {
		g.log.Debugf("INFO: %v was rejected. (blocklisted)", addr)
		conn.Close()
		return
	}
	remoteVersion, err := acceptVersionHandshake(conn, ProtocolVersion)
	if err != nil {
		g.log.Debugf("INFO: %v wanted to connect but version handshake failed: %v", addr, err)
		conn.Close()
		return
	}

	if err = acceptableVersion(remoteVersion); err == nil {
		err = g.managedAcceptConnPeer(conn, remoteVersion)
	}
	if err != nil {
		g.log.Debugf("INFO: %v wanted to connect, but failed: %v", addr, err)
		conn.Close()
		return
	}

	// Handshake successful, remove the deadline.
	conn.SetDeadline(time.Time{})

	g.log.Debugf("INFO: accepted connection from new peer %v (v%v)", addr, remoteVersion)
}

// acceptableSessionHeader returns an error if remoteHeader indicates a peer
// that should not be connected to.
func acceptableSessionHeader(ourHeader, remoteHeader sessionHeader, remoteAddr string) error {
	if remoteHeader.GenesisID != ourHeader.GenesisID {
		return errPeerGenesisID
	} else if remoteHeader.UniqueID == ourHeader.UniqueID {
		return errOurAddress
	} else if err := remoteHeader.NetAddress.IsStdValid(); err != nil {
		return fmt.Errorf("invalid remote address: %v", err)
	}
	return nil
}

// managedAcceptConnPeer accepts connection requests from peers >= v1.3.1.
// The requesting peer is added as a node and a peer. The peer is only added if
// a nil error is returned.
func (g *Gateway) managedAcceptConnPeer(conn net.Conn, remoteVersion string) error {
	g.log.Debugln("Attempting to Accept Connection from Peer; Sending sessionHeader with address", g.myAddr, g.myAddr.IsLocal())
	// Perform header handshake.
	g.mu.RLock()
	ourHeader := sessionHeader{
		GenesisID:  types.GenesisID,
		UniqueID:   g.staticID,
		NetAddress: g.myAddr,
	}
	rl := g.rl
	g.mu.RUnlock()

	remoteHeader, err := exchangeRemoteHeader(conn, ourHeader)
	if err != nil {
		g.log.Debugln("Unable to Accept Connection with Peer. Conn, err:", conn.RemoteAddr(), conn.LocalAddr(), err)
		return err
	}
	if err := exchangeOurHeader(conn, ourHeader); err != nil {
		g.log.Debugln("Unable to Accept Connection with Peer. Conn, err:", conn.RemoteAddr(), conn.LocalAddr(), err)
		return err
	}

	// Get the remote address on which the connecting peer is listening on.
	// This means we need to combine the incoming connections ip address with
	// the announced open port of the peer.
	remoteIP := modules.NetAddress(conn.RemoteAddr().String()).Host()
	remotePort := remoteHeader.NetAddress.Port()
	remoteAddr := modules.NetAddress(net.JoinHostPort(remoteIP, remotePort))
	g.log.Debugln("Making connection with remote peer", remoteAddr)

	// Accept the peer.
	peer := &peer{
		Peer: modules.Peer{
			Inbound: true,
			// NOTE: local may be true even if the supplied NetAddress is not
			// actually reachable.
			Local: remoteAddr.IsLocal(),
			// Ignoring claimed IP address (which should be == to the socket address)
			// by the host but keeping note of the port number so we can call back
			NetAddress: remoteAddr,
			Version:    remoteVersion,
		},
		m:    g.m,
		rl:   rl,
		sess: newServerStream(conn, remoteVersion),
	}
	g.mu.Lock()
	g.acceptPeer(peer)
	g.mu.Unlock()

	// Attempt to ping the supplied address. If successful, we will add
	// remoteHeader.NetAddress to our node list after accepting the peer. We
	// do this in a goroutine so that we can begin communicating with the peer
	// immediately.
	go func() {
		err := g.staticPingNode(remoteAddr)
		if err == nil {
			g.mu.Lock()
			g.addNode(remoteAddr)
			g.mu.Unlock()
		}
	}()

	return nil
}

// acceptPeer makes room for the peer if necessary by kicking out existing
// peers, then adds the peer to the peer list.
func (g *Gateway) acceptPeer(p *peer) {
	// If we are not fully connected, add the peer without kicking any out.
	if len(g.peers) < fullyConnectedThreshold {
		g.addPeer(p)
		return
	}

	// Select a peer to kick. Outbound peers and local peers are not
	// available to be kicked.
	var addrs, preferredAddrs []modules.NetAddress
	for addr, peer := range g.peers {
		// Do not kick outbound peers or local peers.
		if !peer.Inbound || peer.Local {
			continue
		}

		// Prefer kicking a peer with the same hostname.
		if addr.Host() == p.NetAddress.Host() {
			preferredAddrs = append(preferredAddrs, addr)
			continue
		}
		addrs = append(addrs, addr)
	}
	if len(preferredAddrs) > 0 {
		// If there are preferredAddrs we choose randomly from them.
		addrs = preferredAddrs
	}
	if len(addrs) == 0 {
		// There is nobody suitable to kick, therefore do not kick anyone.
		g.addPeer(p)
		return
	}

	// Of the remaining options, select one at random.
	kick := addrs[fastrand.Intn(len(addrs))]

	g.peers[kick].sess.Close()
	delete(g.peers, kick)
	g.log.Printf("INFO: disconnected from %v to make room for %v\n", kick, p.NetAddress)
	g.addPeer(p)
}

// acceptableVersion returns an error if the version is unacceptable.
func acceptableVersion(version string) error {
	if !build.IsVersion(version) {
		return invalidVersionError(version)
	}
	if build.VersionCmp(version, minimumAcceptablePeerVersion) < 0 {
		return insufficientVersionError(version)
	}
	return nil
}

// connectVersionHandshake performs the version handshake and should be called
// on the side making the connection request. The remote version is only
// returned if err == nil.
func connectVersionHandshake(conn net.Conn, version string) (remoteVersion string, err error) {
	// Send our version.
	if err := encoding.WriteObject(conn, version); err != nil {
		return "", fmt.Errorf("failed to write version: %v", err)
	}
	// Read remote version.
	if err := encoding.ReadObject(conn, &remoteVersion, build.MaxEncodedVersionLength); err != nil {
		return "", fmt.Errorf("failed to read remote version: %v", err)
	}
	// Check that their version is acceptable.
	if remoteVersion == "reject" {
		return "", errPeerRejectedConn
	}
	if err := acceptableVersion(remoteVersion); err != nil {
		return "", err
	}
	return remoteVersion, nil
}

// acceptVersionHandshake performs the version handshake and should be
// called on the side accepting a connection request. The remote version is
// only returned if err == nil.
func acceptVersionHandshake(conn net.Conn, version string) (remoteVersion string, err error) {
	// Read remote version.
	if err := encoding.ReadObject(conn, &remoteVersion, build.MaxEncodedVersionLength); err != nil {
		return "", fmt.Errorf("failed to read remote version: %v", err)
	}
	// Check that their version is acceptable.
	if err := acceptableVersion(remoteVersion); err != nil {
		if err := encoding.WriteObject(conn, "reject"); err != nil {
			return "", fmt.Errorf("failed to write reject: %v", err)
		}
		return "", err
	}
	// Send our version.
	if err := encoding.WriteObject(conn, version); err != nil {
		return "", fmt.Errorf("failed to write version: %v", err)
	}
	return remoteVersion, nil
}

// exchangeOurHeader writes ourHeader and reads the remote's error response.
func exchangeOurHeader(conn net.Conn, ourHeader sessionHeader) error {
	// Send our header.
	if err := encoding.WriteObject(conn, ourHeader); err != nil {
		return fmt.Errorf("failed to write header: %v", err)
	}

	// Read remote response.
	var response string
	if err := encoding.ReadObject(conn, &response, 100); err != nil {
		return fmt.Errorf("failed to read header acceptance: %v", err)
	} else if response == modules.StopResponse {
		return errors.New("peer did not want a connection")
	} else if response != modules.AcceptResponse {
		return fmt.Errorf("peer rejected our header: %v", response)
	}
	return nil
}

// exchangeRemoteHeader reads the remote header and writes an error response.
func exchangeRemoteHeader(conn net.Conn, ourHeader sessionHeader) (sessionHeader, error) {
	// Read remote header.
	var remoteHeader sessionHeader
	if err := encoding.ReadObject(conn, &remoteHeader, maxEncodedSessionHeaderSize); err != nil {
		return sessionHeader{}, fmt.Errorf("failed to read remote header: %v", err)
	}

	// Validate remote header and write acceptance or rejection.
	err := acceptableSessionHeader(ourHeader, remoteHeader, conn.RemoteAddr().String())
	if err != nil {
		encoding.WriteObject(conn, err.Error()) // error can be ignored
		return sessionHeader{}, fmt.Errorf("peer's header was not acceptable: %v", err)
	} else if err := encoding.WriteObject(conn, modules.AcceptResponse); err != nil {
		return sessionHeader{}, fmt.Errorf("failed to write header acceptance: %v", err)
	}

	return remoteHeader, nil
}

// managedConnectPeer connects to peers >= v1.3.1. The peer is added as a
// node and a peer. The peer is only added if a nil error is returned.
func (g *Gateway) managedConnectPeer(conn net.Conn, remoteVersion string, remoteAddr modules.NetAddress) error {
	g.log.Debugln("Sending sessionHeader with address", g.myAddr, g.myAddr.IsLocal())
	// Perform header handshake.
	g.mu.RLock()
	ourHeader := sessionHeader{
		GenesisID:  types.GenesisID,
		UniqueID:   g.staticID,
		NetAddress: g.myAddr,
	}
	g.mu.RUnlock()

	if err := exchangeOurHeader(conn, ourHeader); err != nil {
		return err
	} else if _, err := exchangeRemoteHeader(conn, ourHeader); err != nil {
		return err
	}
	return nil
}

// managedConnect establishes a persistent connection to a peer, and adds it to
// the Gateway's peer list.
func (g *Gateway) managedConnect(addr modules.NetAddress) error {
	g.log.Debugln("Attempting to connect to", addr)
	// Perform verification on the input address.
	g.mu.RLock()
	gaddr := g.myAddr
	g.mu.RUnlock()
	if addr == gaddr {
		err := errors.New("can't connect to our own address")
		g.log.Debugln("Unable to connect to", addr, "error:", err)
		return err
	}
	if err := addr.IsStdValid(); err != nil {
		err := errors.New("can't connect to invalid address")
		g.log.Debugln("Unable to connect to", addr, "error:", err)
		return err
	}
	if net.ParseIP(addr.Host()) == nil {
		err := errors.New("address must be an IP address")
		g.log.Debugln("Unable to connect to", addr, "error:", err)
		return err
	}
	if _, exists := g.blocklist[addr.Host()]; exists {
		err := errors.New("can't connect to blocklisted address")
		g.log.Debugln("Unable to connect to", addr, "error:", err)
		return err
	}
	g.mu.RLock()
	_, exists := g.peers[addr]
	g.mu.RUnlock()
	if exists {
		g.log.Debugln("Unable to connect to", addr, "error:", errPeerExists)
		return errPeerExists
	}

	// Dial the peer and perform peer initialization.
	conn, err := g.staticDial(addr)
	if err != nil {
		g.log.Debugln("Unable to connect to", addr, "error:", err)
		return err
	}
	g.log.Debugln("Created conn; remote and local addr", conn.RemoteAddr(), conn.LocalAddr())

	// Perform peer initialization.
	remoteVersion, err := connectVersionHandshake(conn, ProtocolVersion)
	if err != nil {
		conn.Close()
		g.log.Debugln("Unable to connect to", addr, "error:", err)
		return err
	}

	if err = acceptableVersion(remoteVersion); err == nil {
		err = g.managedConnectPeer(conn, remoteVersion, addr)
	}
	if err != nil {
		conn.Close()
		g.log.Debugln("Unable to connect to", addr, "error:", err)
		return err
	}

	// Connection successful, clear the timeout as to maintain a persistent
	// connection to this peer.
	conn.SetDeadline(time.Time{})

	// Add the peer.
	g.mu.Lock()
	defer g.mu.Unlock()

	g.addPeer(&peer{
		Peer: modules.Peer{
			Inbound:    false,
			Local:      addr.IsLocal(),
			NetAddress: addr,
			Version:    remoteVersion,
		},
		m:    g.m,
		rl:   g.rl,
		sess: newClientStream(conn, remoteVersion),
	})
	g.addNode(addr)
	g.nodes[addr].WasOutboundPeer = true

	if err := g.saveSyncNodes(); err != nil {
		g.log.Println("ERROR: Unable to save new outbound peer to gateway:", err)
	}

	g.log.Debugln("INFO: connected to new peer", addr)

	// call initRPCs
	g.callInitRPCs(addr)

	return nil
}

// Connect establishes a persistent connection to a peer, and adds it to the
// Gateway's peer list.
func (g *Gateway) Connect(addr modules.NetAddress) error {
	if err := g.threads.Add(); err != nil {
		return err
	}
	defer g.threads.Done()
	return g.managedConnect(addr)
}

// Disconnect terminates a connection to a peer and removes it from the
// Gateway's peer list.
func (g *Gateway) Disconnect(addr modules.NetAddress) error {
	g.log.Debugln("Attempting to Disconnect from", addr)
	if err := g.threads.Add(); err != nil {
		return err
	}
	defer g.threads.Done()

	g.mu.RLock()
	p, exists := g.peers[addr]
	g.mu.RUnlock()
	if !exists {
		err := errors.New("not connected to that node")
		g.log.Debugln("Unable to disconnect to", addr, "error:", err)
		return err
	}

	p.sess.Close()
	g.mu.Lock()
	// Peer is removed from the peer list as well as the node list, to prevent
	// the node from being re-connected while looking for a replacement peer.
	delete(g.peers, addr)
	delete(g.nodes, addr)
	g.mu.Unlock()

	g.log.Println("INFO: disconnected from peer", addr)
	return nil
}

// ConnectManual is a wrapper for the Connect function. It is specifically used
// if a user wants to connect to a node manually. This also removes the node
// from the blocklist.
func (g *Gateway) ConnectManual(addr modules.NetAddress) error {
	g.log.Debugln("Attempting to Manually Connect to", addr)
	g.mu.Lock()
	var err error
	if _, exists := g.blocklist[addr.Host()]; exists {
		g.log.Debugln("Removing", addr, "from the blocklist due to Manually trying to Connect")
		delete(g.blocklist, addr.Host())
		err = g.saveSync()
	}
	g.mu.Unlock()
	return build.ComposeErrors(err, g.Connect(addr))
}

// DisconnectManual is a wrapper for the Disconnect function. It is
// specifically used if a user wants to connect to a node manually. This also
// adds the node to the blocklist.
func (g *Gateway) DisconnectManual(addr modules.NetAddress) error {
	g.log.Debugln("Attempting to Manually Disconnect from", addr)
	err := g.Disconnect(addr)
	if err == nil {
		g.log.Debugln("Adding", addr, "to the blocklist because of successful manual disconnect")
		g.mu.Lock()
		g.blocklist[addr.Host()] = struct{}{}
		err = g.saveSync()
		g.mu.Unlock()
	}
	return err
}

// Online returns true if the node is connected to the internet. During testing
// we always assume that the node is online
func (g *Gateway) Online() (online bool) {
	defer func() {
		if online {
			g.staticAlerter.UnregisterAlert(modules.AlertIDGatewayOffline)
		} else {
			g.staticAlerter.RegisterAlert(modules.AlertIDGatewayOffline, AlertMSGGatewayOffline, "", modules.SeverityWarning)
		}
	}()
	disableAutoOnline := g.staticDeps.Disrupt("DisableGatewayAutoOnline")
	if (build.Release == "dev" || build.Release == "testing") && !disableAutoOnline {
		return true
	}

	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, p := range g.peers {
		if !p.Local || disableAutoOnline {
			return true
		}
	}
	return false
}

// Peers returns the addresses currently connected to the Gateway.
func (g *Gateway) Peers() []modules.Peer {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var peers []modules.Peer
	for _, p := range g.peers {
		peers = append(peers, p.Peer)
	}
	return peers
}
