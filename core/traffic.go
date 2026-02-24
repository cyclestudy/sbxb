package core

import (
	"context"
	"log/slog"
	"net"
	"strconv"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/cyclestudy/sbxb/common/counter"
)

// TrafficTracker implements adapter.ConnectionTracker to count per-user
// traffic. It is registered with the sing-box router via
// router.SetTracker().
type TrafficTracker struct {
	storage *counter.TrafficStorage
}

// NewTrafficTracker creates a new TrafficTracker with its own storage.
func NewTrafficTracker() *TrafficTracker {
	return &TrafficTracker{
		storage: counter.NewTrafficStorage(),
	}
}

// RoutedConnection wraps a connection to count uploaded and downloaded
// bytes for the user identified by metadata.User (set to
// strconv.Itoa(userID)). If no user is set the connection passes
// through unwrapped.
func (t *TrafficTracker) RoutedConnection(
	ctx context.Context,
	conn net.Conn,
	metadata adapter.InboundContext,
	matchedRule adapter.Rule,
	matchOutbound adapter.Outbound,
) net.Conn {
	if metadata.User == "" {
		return conn
	}
	return &countConn{
		Conn:    conn,
		user:    metadata.User,
		storage: t.storage,
	}
}

// RoutedPacketConnection wraps a packet connection to count uploaded and
// downloaded bytes for the user.
func (t *TrafficTracker) RoutedPacketConnection(
	ctx context.Context,
	conn N.PacketConn,
	metadata adapter.InboundContext,
	matchedRule adapter.Rule,
	matchOutbound adapter.Outbound,
) N.PacketConn {
	if metadata.User == "" {
		return conn
	}
	return &countPacketConn{
		PacketConn: conn,
		user:       metadata.User,
		storage:    t.storage,
	}
}

// GetTraffic resets all counters and returns a map of user ID to
// [upload, download] byte totals. Users whose tag cannot be parsed as
// an integer are logged and skipped.
func (t *TrafficTracker) GetTraffic() map[int][2]int64 {
	raw := t.storage.ResetAll()
	result := make(map[int][2]int64, len(raw))
	for tag, data := range raw {
		uid, err := strconv.Atoi(tag)
		if err != nil {
			slog.Warn("traffic: failed to parse user tag", "tag", tag, "error", err)
			continue
		}
		result[uid] = data
	}
	return result
}

// RestoreTraffic adds back traffic that failed to be reported.
// This prevents data loss when the panel API is unreachable.
func (t *TrafficTracker) RestoreTraffic(data map[int][2]int64) {
	for uid, amounts := range data {
		name := strconv.Itoa(uid)
		c := t.storage.GetOrCreate(name)
		c.AddUpload(amounts[0])
		c.AddDownload(amounts[1])
	}
}

// ---------------------------------------------------------------------------
// countConn wraps net.Conn to count bytes per Read/Write.
// ---------------------------------------------------------------------------

type countConn struct {
	net.Conn
	user    string
	storage *counter.TrafficStorage
}

func (c *countConn) Read(b []byte) (n int, err error) {
	n, err = c.Conn.Read(b)
	if n > 0 {
		c.storage.GetOrCreate(c.user).AddDownload(int64(n))
	}
	return
}

func (c *countConn) Write(b []byte) (n int, err error) {
	n, err = c.Conn.Write(b)
	if n > 0 {
		c.storage.GetOrCreate(c.user).AddUpload(int64(n))
	}
	return
}

// Upstream returns the inner connection, allowing sing-box's
// common.Cast to traverse the wrapper chain and discover protocol-
// specific interfaces like HandshakeSuccess (needed by Hysteria2/TUIC).
func (c *countConn) Upstream() any {
	return c.Conn
}

// ---------------------------------------------------------------------------
// countPacketConn wraps N.PacketConn to count bytes per packet.
// ---------------------------------------------------------------------------

type countPacketConn struct {
	N.PacketConn
	user    string
	storage *counter.TrafficStorage
}

func (c *countPacketConn) ReadPacket(buffer *buf.Buffer) (destination M.Socksaddr, err error) {
	destination, err = c.PacketConn.ReadPacket(buffer)
	if err == nil {
		c.storage.GetOrCreate(c.user).AddDownload(int64(buffer.Len()))
	}
	return
}

func (c *countPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	n := buffer.Len()
	err := c.PacketConn.WritePacket(buffer, destination)
	if err == nil {
		c.storage.GetOrCreate(c.user).AddUpload(int64(n))
	}
	return err
}

// Upstream returns the inner packet connection for wrapper traversal.
func (c *countPacketConn) Upstream() any {
	return c.PacketConn
}
