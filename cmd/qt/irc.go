// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxIRCMessages = 500

type ircConfig struct {
	ServerAddr string
	Channel    string
	NickPrefix string
}

type ircMessage struct {
	Time   string
	Sender string
	Text   string
	Self   bool
	System bool
}

type ircClient struct {
	cfg ircConfig

	mu         sync.RWMutex
	conn       net.Conn
	reader     *bufio.Reader
	writer     *bufio.Writer
	connected  bool
	connecting bool
	joined     bool
	nick       string
	topic      string
	lastErr    string
	messages   []ircMessage
	users      map[string]struct{}
	lastSent   string
	lastSentAt time.Time
}

func newIRCClient(cfg ircConfig) *ircClient {
	if cfg.ServerAddr == "" {
		cfg.ServerAddr = "irc.libera.chat:6697"
	}
	if cfg.Channel == "" {
		cfg.Channel = "#test112221"
	}
	if cfg.NickPrefix == "" {
		cfg.NickPrefix = "wallet"
	}

	return &ircClient{
		cfg:   cfg,
		users: make(map[string]struct{}),
	}
}

func (c *ircClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.connected || c.connecting {
		c.mu.Unlock()
		return nil
	}
	c.connecting = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.connecting = false
		c.mu.Unlock()
	}()

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", c.cfg.ServerAddr, &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: strings.Split(c.cfg.ServerAddr, ":")[0],
	})
	if err != nil {
		c.setErrorLocked(fmt.Sprintf("dial failed: %v", err))
		return err
	}

	nick := buildIRCNick(c.cfg.NickPrefix)
	reader := bufio.NewReaderSize(conn, 64*1024)
	writer := bufio.NewWriterSize(conn, 64*1024)

	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = conn
	c.reader = reader
	c.writer = writer
	c.nick = nick
	c.connected = true
	c.joined = false
	c.topic = ""
	c.users = make(map[string]struct{})
	c.lastErr = ""
	c.appendSystemLocked("Connected to " + c.cfg.ServerAddr)
	c.mu.Unlock()

	if err := c.sendRaw("NICK " + nick); err != nil {
		c.markDisconnected(fmt.Sprintf("failed to send NICK: %v", err))
		return err
	}
	if err := c.sendRaw("USER " + nick + " 0 * :" + nick + " wallet"); err != nil {
		c.markDisconnected(fmt.Sprintf("failed to send USER: %v", err))
		return err
	}

	go c.readLoop()
	return nil
}

func (c *ircClient) Close() {
	c.markDisconnected("Disconnected")
}

func (c *ircClient) SendMessage(text string) error {
	msg := strings.TrimSpace(text)
	if msg == "" {
		return fmt.Errorf("message is empty")
	}
	if len(msg) > 400 {
		return fmt.Errorf("message too long")
	}

	c.mu.RLock()
	connected := c.connected
	nick := c.nick
	channel := c.cfg.Channel
	joined := c.joined
	c.mu.RUnlock()
	if !connected {
		return fmt.Errorf("not connected to IRC")
	}
	if !joined {
		return fmt.Errorf("not yet joined %s", channel)
	}

	if err := c.sendRaw("PRIVMSG " + channel + " :" + msg); err != nil {
		c.markDisconnected(fmt.Sprintf("send failed: %v", err))
		return err
	}

	c.mu.Lock()
	c.appendMessageLocked(ircMessage{
		Time:   time.Now().UTC().Format(time.RFC3339),
		Sender: nick,
		Text:   msg,
		Self:   true,
	})
	c.lastSent = msg
	c.lastSentAt = time.Now()
	c.mu.Unlock()
	return nil
}

func (c *ircClient) ChangeNick(newNick string) error {
	nick := strings.TrimSpace(newNick)
	if nick == "" {
		return fmt.Errorf("nickname cannot be empty")
	}
	if len(nick) > 16 {
		return fmt.Errorf("nickname too long (max 16 characters)")
	}
	for _, r := range nick {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return fmt.Errorf("nickname contains invalid character %q (use letters, digits, _ or -)", string(r))
		}
	}
	if nick[0] >= '0' && nick[0] <= '9' {
		return fmt.Errorf("nickname cannot start with a digit")
	}

	c.mu.RLock()
	connected := c.connected
	current := c.nick
	c.mu.RUnlock()
	if !connected {
		return fmt.Errorf("not connected to IRC")
	}
	if nick == current {
		return nil
	}

	return c.sendRaw("NICK " + nick)
}

func (c *ircClient) Status() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"connected": c.connected,
		"joined":    c.joined,
		"server":    c.cfg.ServerAddr,
		"channel":   c.cfg.Channel,
		"nick":      c.nick,
		"topic":     c.topic,
		"userCount": len(c.users),
		"error":     c.lastErr,
	}
}

func (c *ircClient) Messages() []map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]map[string]interface{}, 0, len(c.messages))
	for _, m := range c.messages {
		out = append(out, map[string]interface{}{
			"time":   m.Time,
			"sender": m.Sender,
			"text":   m.Text,
			"self":   m.Self,
			"system": m.System,
		})
	}
	return out
}

func (c *ircClient) Users() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	users := make([]string, 0, len(c.users))
	for u := range c.users {
		users = append(users, u)
	}
	sort.Slice(users, func(i, j int) bool {
		return strings.ToLower(users[i]) < strings.ToLower(users[j])
	})
	return users
}

func (c *ircClient) readLoop() {
	for {
		c.mu.RLock()
		connected := c.connected
		reader := c.reader
		c.mu.RUnlock()
		if !connected || reader == nil {
			return
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			c.markDisconnected(fmt.Sprintf("read failed: %v", err))
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		c.handleLine(line)
	}
}

func (c *ircClient) handleLine(line string) {
	if strings.HasPrefix(line, "PING :") {
		_ = c.sendRaw("PONG :" + strings.TrimPrefix(line, "PING :"))
		return
	}

	prefix, cmd, args, trailing := parseIRCLine(line)
	switch cmd {
	case "001":
		_ = c.sendRaw("JOIN " + c.cfg.Channel)
		c.mu.Lock()
		c.appendSystemLocked("Joining " + c.cfg.Channel + " as " + c.nick)
		c.mu.Unlock()
	case "433":
		newNick := buildIRCNick(c.cfg.NickPrefix)
		c.mu.Lock()
		c.nick = newNick
		c.appendSystemLocked("Nickname in use, switching to " + newNick)
		c.mu.Unlock()
		_ = c.sendRaw("NICK " + newNick)
	case "332":
		// RPL_TOPIC: <nick> <channel> :<topic>
		if len(args) >= 2 && channelEqual(args[1], c.cfg.Channel) {
			c.mu.Lock()
			c.topic = trailing
			if trailing == "" {
				c.appendSystemLocked("Topic: (none)")
			} else {
				c.appendSystemLocked("Topic: " + trailing)
			}
			c.mu.Unlock()
		}
	case "353":
		// RPL_NAMREPLY: <nick> <symbol> <channel> :nicklist
		if len(args) >= 3 && channelEqual(args[2], c.cfg.Channel) {
			names := strings.Fields(trailing)
			c.mu.Lock()
			for _, name := range names {
				n := normalizeNick(name)
				if n != "" {
					c.users[n] = struct{}{}
				}
			}
			c.mu.Unlock()
		}
	case "366":
		// RPL_ENDOFNAMES: <nick> <channel> :End of /NAMES list.
		if len(args) >= 2 && channelEqual(args[1], c.cfg.Channel) {
			c.mu.Lock()
			c.joined = true
			c.users[c.nick] = struct{}{}
			c.appendSystemLocked(fmt.Sprintf("Joined %s (%d users)", c.cfg.Channel, len(c.users)))
			c.mu.Unlock()
		}
	case "TOPIC":
		// :nick!ident@host TOPIC #chan :new topic
		if len(args) >= 1 && channelEqual(args[0], c.cfg.Channel) {
			sender := parseSender(prefix)
			c.mu.Lock()
			c.topic = trailing
			if trailing == "" {
				c.appendSystemLocked(sender + " cleared the topic")
			} else {
				c.appendSystemLocked(sender + " set topic: " + trailing)
			}
			c.mu.Unlock()
		}
	case "JOIN":
		channel := trailing
		if channel == "" && len(args) >= 1 {
			channel = args[0]
		}
		if !channelEqual(channel, c.cfg.Channel) {
			return
		}
		sender := parseSender(prefix)
		if sender == "" {
			return
		}
		c.mu.Lock()
		c.users[sender] = struct{}{}
		if sender != c.nick {
			c.appendSystemLocked(sender + " joined")
		}
		c.mu.Unlock()
	case "PART":
		if len(args) < 1 || !channelEqual(args[0], c.cfg.Channel) {
			return
		}
		sender := parseSender(prefix)
		if sender == "" {
			return
		}
		c.mu.Lock()
		delete(c.users, sender)
		if sender != c.nick {
			c.appendSystemLocked(sender + " left")
		}
		c.mu.Unlock()
	case "QUIT":
		sender := parseSender(prefix)
		if sender == "" {
			return
		}
		c.mu.Lock()
		if _, ok := c.users[sender]; ok {
			delete(c.users, sender)
			if sender != c.nick {
				c.appendSystemLocked(sender + " quit")
			}
		}
		c.mu.Unlock()
	case "NICK":
		sender := parseSender(prefix)
		newNick := normalizeNick(trailing)
		if sender == "" || newNick == "" {
			return
		}
		c.mu.Lock()
		if _, ok := c.users[sender]; ok {
			delete(c.users, sender)
			c.users[newNick] = struct{}{}
		}
		if sender == c.nick {
			c.nick = newNick
		}
		c.appendSystemLocked(sender + " is now known as " + newNick)
		c.mu.Unlock()
	case "NOTICE":
		sender := parseSender(prefix)
		if sender == "" {
			sender = "notice"
		}
		if trailing == "" {
			return
		}
		c.mu.Lock()
		c.appendSystemLocked(sender + ": " + trailing)
		c.mu.Unlock()
	case "PRIVMSG":
		if len(args) == 0 {
			return
		}
		target := args[0]
		if target != c.cfg.Channel {
			return
		}
		sender := parseSender(prefix)
		if sender == "" {
			sender = "unknown"
		}
		c.mu.Lock()
		// Avoid showing our own outbound message twice when server echoes it.
		if sender == c.nick && trailing == c.lastSent && time.Since(c.lastSentAt) < 5*time.Second {
			c.lastSent = ""
			c.lastSentAt = time.Time{}
			c.mu.Unlock()
			return
		}
		c.appendMessageLocked(ircMessage{
			Time:   time.Now().UTC().Format(time.RFC3339),
			Sender: sender,
			Text:   trailing,
			Self:   sender == c.nick,
		})
		c.mu.Unlock()
	}
}

func (c *ircClient) sendRaw(line string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.writer == nil {
		return fmt.Errorf("not connected")
	}
	if _, err := c.writer.WriteString(line + "\r\n"); err != nil {
		return err
	}
	return c.writer.Flush()
}

func (c *ircClient) setErrorLocked(msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastErr = msg
}

func (c *ircClient) markDisconnected(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = nil
	c.reader = nil
	c.writer = nil
	c.joined = false
	c.users = make(map[string]struct{})
	wasConnected := c.connected
	c.connected = false
	if reason != "" {
		c.lastErr = reason
	}
	if wasConnected && reason != "" {
		c.appendSystemLocked(reason)
	}
}

func (c *ircClient) appendSystemLocked(text string) {
	c.appendMessageLocked(ircMessage{
		Time:   time.Now().UTC().Format(time.RFC3339),
		Sender: "system",
		Text:   text,
		System: true,
	})
}

func (c *ircClient) appendMessageLocked(msg ircMessage) {
	c.messages = append(c.messages, msg)
	if len(c.messages) > maxIRCMessages {
		c.messages = c.messages[len(c.messages)-maxIRCMessages:]
	}
}

func parseIRCLine(line string) (prefix, cmd string, args []string, trailing string) {
	rest := line
	if strings.HasPrefix(rest, ":") {
		if idx := strings.IndexByte(rest, ' '); idx > 1 {
			prefix = rest[1:idx]
			rest = strings.TrimSpace(rest[idx+1:])
		}
	}

	if idx := strings.Index(rest, " :"); idx >= 0 {
		trailing = rest[idx+2:]
		rest = rest[:idx]
	}

	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return prefix, "", nil, trailing
	}
	cmd = fields[0]
	if len(fields) > 1 {
		args = fields[1:]
	}
	return prefix, cmd, args, trailing
}

func parseSender(prefix string) string {
	if prefix == "" {
		return ""
	}
	if idx := strings.IndexByte(prefix, '!'); idx >= 0 {
		return prefix[:idx]
	}
	return prefix
}

func normalizeNick(name string) string {
	n := strings.TrimSpace(name)
	n = strings.TrimLeft(n, "@+%&~")
	return n
}

func channelEqual(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func buildIRCNick(prefix string) string {
	base := strings.ToLower(prefix)
	base = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, base)
	if base == "" {
		base = "wallet"
	}
	if len(base) > 8 {
		base = base[:8]
	}

	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err != nil {
		return base + "x1"
	}
	return base + hex.EncodeToString(buf)
}
