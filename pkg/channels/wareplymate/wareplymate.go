package wareplymate

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/sipeed/freeclaw/pkg/bus"
	"github.com/sipeed/freeclaw/pkg/channels"
	"github.com/sipeed/freeclaw/pkg/config"
	"github.com/sipeed/freeclaw/pkg/identity"
	"github.com/sipeed/freeclaw/pkg/logger"
	"github.com/sipeed/freeclaw/pkg/utils"
)

const (
	defaultPollIntervalSeconds  = 1
	maxPollBatch                = 200
	defaultSelfCommandPrefix    = "/atharva"
	defaultSessionRetentionDays = 150
	defaultBridgeStartupTimeout = 20
	defaultOwnerActiveWindowSec = 70
	outboundEchoTTL             = 2 * time.Minute
)

type pendingInbound struct {
	messageID string
	cancel    context.CancelFunc
}

type WAReplyMateChannel struct {
	*channels.BaseChannel

	cfg    config.WAReplyMateConfig
	client *http.Client

	ctx    context.Context
	cancel context.CancelFunc

	mu              sync.Mutex
	db              *sql.DB
	lastRowID       int64
	maxRowIDAtStart int64
	ownerIDs        map[string]struct{}
	pendingByChat   map[string]*pendingInbound
	ownerLastActive map[string]time.Time
	recentOutbound  map[string]time.Time

	bridgeCmd     *exec.Cmd
	bridgeStarted bool
}

type dbMessage struct {
	rowID     int64
	messageID string
	senderID  string
	chatID    string
	content   string
	chatName  string
	mediaType string
	timestamp string
	isFromMe  int64
}

type sendPayload struct {
	Recipient string `json:"recipient"`
	Message   string `json:"message"`
}

type sendResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func NewWAReplyMateChannel(cfg config.WAReplyMateConfig, b *bus.MessageBus) (*WAReplyMateChannel, error) {
	base := channels.NewBaseChannel(
		"wareplymate",
		cfg,
		b,
		cfg.AllowFrom,
		channels.WithMaxMessageLength(65536),
		channels.WithReasoningChannelID(cfg.ReasoningChannelID),
	)

	apiBaseURL := strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = "http://localhost:8080/api"
	}
	cfg.APIBaseURL = apiBaseURL
	if strings.TrimSpace(cfg.SelfCommandPrefix) == "" {
		cfg.SelfCommandPrefix = defaultSelfCommandPrefix
	}
	if cfg.SessionRetentionDays <= 0 {
		cfg.SessionRetentionDays = defaultSessionRetentionDays
	}
	if cfg.BridgeStartupTimeout <= 0 {
		cfg.BridgeStartupTimeout = defaultBridgeStartupTimeout
	}
	if cfg.NonOwnerDelaySeconds < 0 {
		cfg.NonOwnerDelaySeconds = 0
	}
	if cfg.OwnerActiveWindowSec <= 0 {
		cfg.OwnerActiveWindowSec = defaultOwnerActiveWindowSec
	}

	ownerIDs := make(map[string]struct{})
	for _, jid := range cfg.OwnerJIDs {
		normalized := normalizeJID(string(jid))
		if normalized != "" {
			ownerIDs[normalized] = struct{}{}
		}
	}

	return &WAReplyMateChannel{
		BaseChannel:     base,
		cfg:             cfg,
		ownerIDs:        ownerIDs,
		pendingByChat:   make(map[string]*pendingInbound),
		ownerLastActive: make(map[string]time.Time),
		recentOutbound:  make(map[string]time.Time),
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}, nil
}

func (c *WAReplyMateChannel) Start(ctx context.Context) error {
	if err := c.ensureBridgeReady(ctx); err != nil {
		logger.WarnCF("wareplymate", "Bridge not ready at startup; inbound polling will continue and sends will retry bridge readiness", map[string]any{
			"error": err.Error(),
		})
	}

	if strings.TrimSpace(c.cfg.MessagesDBPath) == "" {
		return fmt.Errorf("wareplymate.messages_db_path is required")
	}

	if _, err := os.Stat(c.cfg.MessagesDBPath); err != nil {
		return fmt.Errorf("messages DB not accessible (%s): %w", c.cfg.MessagesDBPath, err)
	}

	db, err := sql.Open("sqlite", c.cfg.MessagesDBPath)
	if err != nil {
		return fmt.Errorf("open sqlite DB: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = db.PingContext(pingCtx)
	cancel()
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("ping sqlite DB: %w", err)
	}

	lastRowID, err := loadMaxRowID(db)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("load initial row position: %w", err)
	}

	startRowID := applyStartupCatchup(lastRowID, c.cfg.StartupCatchupRows)

	c.mu.Lock()
	c.db = db
	c.lastRowID = startRowID
	c.maxRowIDAtStart = lastRowID
	c.mu.Unlock()

	c.ctx, c.cancel = context.WithCancel(ctx)
	c.SetRunning(true)

	logger.InfoCF("wareplymate", "WAReplyMate channel started", map[string]any{
		"api_base_url":      c.cfg.APIBaseURL,
		"messages_db_path":  c.cfg.MessagesDBPath,
		"poll_interval_sec": c.pollInterval().Seconds(),
		"db_max_row_id":     lastRowID,
		"start_row_id":      startRowID,
		"catchup_rows":      c.cfg.StartupCatchupRows,
	})

	go c.pollLoop()
	return nil
}

func (c *WAReplyMateChannel) Stop(ctx context.Context) error {
	_ = ctx
	if c.cancel != nil {
		c.cancel()
	}

	c.mu.Lock()
	db := c.db
	c.db = nil
	c.mu.Unlock()

	if db != nil {
		if err := db.Close(); err != nil {
			logger.ErrorCF("wareplymate", "Failed to close sqlite DB", map[string]any{"error": err.Error()})
		}
	}

	c.mu.Lock()
	bridgeCmd := c.bridgeCmd
	bridgeStarted := c.bridgeStarted
	pending := c.pendingByChat
	c.bridgeCmd = nil
	c.bridgeStarted = false
	c.pendingByChat = make(map[string]*pendingInbound)
	c.mu.Unlock()

	for _, p := range pending {
		if p != nil && p.cancel != nil {
			p.cancel()
		}
	}

	if bridgeStarted && bridgeCmd != nil && bridgeCmd.Process != nil {
		_ = bridgeCmd.Process.Kill()
	}

	c.SetRunning(false)
	logger.InfoC("wareplymate", "WAReplyMate channel stopped")
	return nil
}

func (c *WAReplyMateChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return channels.ErrNotRunning
	}

	if shouldIgnoreSystemChat(msg.ChatID) {
		logger.InfoCF("wareplymate", "Dropping outbound message to status/update chat", map[string]any{
			"chat": msg.ChatID,
		})
		return nil
	}

	if !c.isBridgeAPIReady() {
		if err := c.ensureBridgeReady(ctx); err != nil {
			return fmt.Errorf("bridge unavailable for send: %w", channels.ErrTemporary)
		}
	}

	payload := sendPayload{
		Recipient: msg.ChatID,
		Message:   msg.Content,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal send payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIBaseURL+"/send", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build send request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("wareplymate send request failed: %w", channels.ErrTemporary)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("wareplymate send returned %d: %w", resp.StatusCode, channels.ErrTemporary)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read send response: %w", err)
	}

	var parsed sendResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("decode send response: %w", err)
	}
	if !parsed.Success {
		logger.WarnCF("wareplymate", "WAReplyMate send rejected", map[string]any{
			"chat_id": msg.ChatID,
			"reason":  parsed.Message,
		})
		return fmt.Errorf("wareplymate send rejected: %w", channels.ErrTemporary)
	}

	c.rememberOutboundEcho(msg.ChatID, msg.Content)

	return nil
}

func (c *WAReplyMateChannel) pollLoop() {
	ticker := time.NewTicker(c.pollInterval())
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if err := c.pollOnce(); err != nil {
				if errors.Is(err, sql.ErrConnDone) {
					logger.ErrorCF("wareplymate", "SQLite connection closed", map[string]any{"error": err.Error()})
					continue
				}
				logger.ErrorCF("wareplymate", "Polling failed", map[string]any{"error": err.Error()})
			}
		}
	}
}

func (c *WAReplyMateChannel) pollOnce() error {
	c.mu.Lock()
	db := c.db
	lastRowID := c.lastRowID
	maxRowIDAtStart := c.maxRowIDAtStart
	c.mu.Unlock()

	if db == nil {
		return nil
	}

	const query = `
SELECT
	m.rowid,
	COALESCE(m.id, ''),
	COALESCE(m.sender, ''),
	COALESCE(m.chat_jid, ''),
	COALESCE(m.content, ''),
	COALESCE(c.name, ''),
	COALESCE(m.media_type, ''),
	COALESCE(m.timestamp, ''),
	COALESCE(m.is_from_me, 0)
FROM messages m
LEFT JOIN chats c ON c.jid = m.chat_jid
WHERE m.rowid > ?
ORDER BY m.rowid ASC
LIMIT ?
`

	rows, err := db.QueryContext(c.ctx, query, lastRowID, maxPollBatch)
	if err != nil {
		return err
	}
	defer rows.Close()

	nextRowID := lastRowID
	for rows.Next() {
		var msg dbMessage
		if err := rows.Scan(
			&msg.rowID,
			&msg.messageID,
			&msg.senderID,
			&msg.chatID,
			&msg.content,
			&msg.chatName,
			&msg.mediaType,
			&msg.timestamp,
			&msg.isFromMe,
		); err != nil {
			return err
		}

		if msg.rowID > nextRowID {
			nextRowID = msg.rowID
		}

		if msg.isFromMe != 0 {
			if c.isLikelyOutboundEcho(msg.chatID, msg.content) {
				continue
			}
			// Process all owner self-messages immediately; prefix is optional.
			msg.content = c.stripSelfCommandPrefix(msg.content)
			if strings.TrimSpace(msg.content) == "" {
				continue
			}
		}
		if strings.TrimSpace(msg.senderID) == "" {
			continue
		}
		if strings.TrimSpace(msg.chatID) == "" {
			msg.chatID = msg.senderID
		}
		if strings.TrimSpace(msg.messageID) == "" {
			msg.messageID = fmt.Sprintf("rowid-%d", msg.rowID)
		}
		if shouldIgnoreSystemChat(msg.chatID) {
			continue
		}
		if strings.TrimSpace(msg.content) == "" && strings.TrimSpace(msg.mediaType) == "" {
			continue
		}
		if c.shouldSkipStaleStartupReplay(msg, maxRowIDAtStart) {
			continue
		}

		metadata := map[string]string{}
		metadata["session_retention_days"] = fmt.Sprintf("%d", c.cfg.SessionRetentionDays)
		if msg.isFromMe != 0 {
			metadata["self_command"] = "true"
		}
		senderRole, roleReason := c.classifySenderRole(msg)
		if senderRole == "owner" {
			metadata["account_id"] = "owner"
			metadata["sender_role"] = "owner"
		} else {
			metadata["account_id"] = "non_owner"
			metadata["sender_role"] = "non_owner"
			metadata["hide_internal_notes"] = "true"
			if c.cfg.NonOwnerNoTools {
				metadata["disable_tools"] = "true"
			}
			if c.cfg.NonOwnerContextHours > 0 {
				metadata["session_bucket_hours"] = fmt.Sprintf("%d", c.cfg.NonOwnerContextHours)
			}
		}
		if msg.chatName != "" {
			metadata["chat_name"] = msg.chatName
		}
		if msg.mediaType != "" {
			metadata["media_type"] = msg.mediaType
		}
		if msg.timestamp != "" {
			metadata["source_timestamp"] = msg.timestamp
		}

		peer := bus.Peer{Kind: "direct", ID: msg.chatID}
		if msg.chatID != msg.senderID {
			peer = bus.Peer{Kind: "group", ID: msg.chatID}
		}

		sender := bus.SenderInfo{
			Platform:    "whatsapp",
			PlatformID:  msg.senderID,
			CanonicalID: identity.BuildCanonicalID("whatsapp", msg.senderID),
		}

		logger.InfoCF("wareplymate", "Inbound message received", map[string]any{
			"sender":      msg.senderID,
			"chat":        msg.chatID,
			"sender_role": senderRole,
			"role_reason": roleReason,
			"preview":     utils.Truncate(msg.content, 60),
		})

		c.dispatchInbound(msg, peer, metadata, sender, senderRole)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	if nextRowID != lastRowID {
		c.mu.Lock()
		if nextRowID > c.lastRowID {
			c.lastRowID = nextRowID
		}
		c.mu.Unlock()
	}

	return nil
}

func (c *WAReplyMateChannel) pollInterval() time.Duration {
	seconds := c.cfg.PollIntervalSeconds
	if seconds <= 0 {
		seconds = defaultPollIntervalSeconds
	}
	return time.Duration(seconds) * time.Second
}

func (c *WAReplyMateChannel) ensureBridgeReady(ctx context.Context) error {
	if c.isBridgeAPIReady() {
		return nil
	}

	if !c.cfg.AutoStartBridge {
		return fmt.Errorf("bridge API is unavailable at %s and auto_start_bridge is disabled", c.cfg.APIBaseURL)
	}

	if err := c.startBridgeProcess(ctx); err != nil {
		return err
	}

	return c.waitForAPI(ctx, time.Duration(c.cfg.BridgeStartupTimeout)*time.Second)
}

func (c *WAReplyMateChannel) startBridgeProcess(ctx context.Context) error {
	c.mu.Lock()
	if c.bridgeStarted {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	command, args, workDir, err := c.resolveBridgeCommand()
	if err != nil {
		return fmt.Errorf("resolve bridge command: %w", err)
	}

	cmd := exec.CommandContext(ctx, command, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start bridge process: %w", err)
	}

	c.mu.Lock()
	c.bridgeCmd = cmd
	c.bridgeStarted = true
	c.mu.Unlock()

	logger.InfoCF("wareplymate", "Started WhatsApp bridge process", map[string]any{
		"command":  command,
		"args":     strings.Join(args, " "),
		"work_dir": workDir,
		"pid":      cmd.Process.Pid,
	})

	go func() {
		err := cmd.Wait()
		if err != nil {
			logger.WarnCF("wareplymate", "Bridge process exited", map[string]any{"error": err.Error()})
		}
	}()

	return nil
}

func (c *WAReplyMateChannel) resolveBridgeCommand() (string, []string, string, error) {
	workDir := strings.TrimSpace(c.cfg.BridgeWorkDir)
	if workDir == "" {
		workDir = c.inferBridgeWorkDir()
	}

	if cmd := strings.TrimSpace(c.cfg.BridgeCommand); cmd != "" {
		return cmd, c.cfg.BridgeArgs, workDir, nil
	}

	if workDir != "" {
		exeName := "whatsapp-bridge"
		if runtime.GOOS == "windows" {
			exeName += ".exe"
		}
		exePath := filepath.Join(workDir, exeName)
		if fi, err := os.Stat(exePath); err == nil && !fi.IsDir() {
			return exePath, nil, workDir, nil
		}

		mainPath := filepath.Join(workDir, "main.go")
		if fi, err := os.Stat(mainPath); err == nil && !fi.IsDir() {
			return "go", []string{"run", "main.go"}, workDir, nil
		}
	}

	return "", nil, "", fmt.Errorf("unable to find bundled bridge binary or main.go; set channels.wareplymate.bridge_command")
}

func (c *WAReplyMateChannel) inferBridgeWorkDir() string {
	dbPath := strings.TrimSpace(c.cfg.MessagesDBPath)
	if dbPath == "" {
		return ""
	}
	storeDir := filepath.Dir(dbPath)
	if strings.EqualFold(filepath.Base(storeDir), "store") {
		return filepath.Dir(storeDir)
	}
	return ""
}

func (c *WAReplyMateChannel) waitForAPI(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if c.isBridgeAPIReady() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("bridge API not reachable at %s after %s", c.cfg.APIBaseURL, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (c *WAReplyMateChannel) isBridgeAPIReady() bool {
	endpoint := strings.TrimRight(c.cfg.APIBaseURL, "/") + "/send"
	req, err := http.NewRequest(http.MethodHead, endpoint, nil)
	if err != nil {
		return false
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// A real bridge endpoint should not be 404. It may legitimately return
	// 2xx/3xx/4xx depending on method support and middleware.
	if resp.StatusCode == http.StatusNotFound {
		return false
	}

	return true
}

func loadMaxRowID(db *sql.DB) (int64, error) {
	var rowID int64
	if err := db.QueryRow("SELECT COALESCE(MAX(rowid), 0) FROM messages").Scan(&rowID); err != nil {
		return 0, err
	}
	return rowID, nil
}

func applyStartupCatchup(maxRowID int64, catchupRows int) int64 {
	if catchupRows <= 0 {
		return maxRowID
	}
	start := maxRowID - int64(catchupRows)
	if start < 0 {
		return 0
	}
	return start
}

func (c *WAReplyMateChannel) shouldSkipStaleStartupReplay(msg dbMessage, maxRowIDAtStart int64) bool {
	if maxRowIDAtStart <= 0 || msg.rowID > maxRowIDAtStart {
		return false
	}
	if c.cfg.StartupCatchupMaxAge <= 0 {
		return false
	}
	ts, ok := parseSourceTimestamp(msg.timestamp)
	if !ok {
		return false
	}
	maxAge := time.Duration(c.cfg.StartupCatchupMaxAge) * time.Second
	return time.Since(ts) > maxAge
}

func parseSourceTimestamp(ts string) (time.Time, bool) {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse("2006-01-02 15:04:05-07:00", ts); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func (c *WAReplyMateChannel) isOwner(sender string) bool {
	if len(c.ownerIDs) == 0 {
		return false
	}
	_, ok := c.ownerIDs[normalizeJID(sender)]
	return ok
}

func (c *WAReplyMateChannel) classifySenderRole(msg dbMessage) (string, string) {
	if msg.isFromMe != 0 {
		return "owner", "self_message"
	}
	if c.isOwner(msg.senderID) {
		return "owner", "sender_jid_match"
	}
	// Intentionally do not classify owner by chat ID to avoid false positives in
	// group/private chat shapes where chat_jid may not uniquely identify sender.
	return "non_owner", "no_owner_match"
}

func (c *WAReplyMateChannel) dispatchInbound(
	msg dbMessage,
	peer bus.Peer,
	metadata map[string]string,
	sender bus.SenderInfo,
	senderRole string,
) {
	if senderRole == "owner" {
		c.noteOwnerActivity(msg.chatID)
		c.cancelPendingForChat(msg.chatID)
		c.HandleMessage(c.ctx, peer, msg.messageID, msg.senderID, msg.chatID, msg.content, nil, metadata, sender)
		return
	}

	if c.cfg.NonOwnerDelaySeconds <= 0 {
		if c.ownerRecentlyActive(msg.chatID) {
			logger.InfoCF("wareplymate", "Skipping non-owner message while owner is active", map[string]any{
				"chat":   msg.chatID,
				"sender": msg.senderID,
			})
			return
		}
		c.HandleMessage(c.ctx, peer, msg.messageID, msg.senderID, msg.chatID, msg.content, nil, metadata, sender)
		return
	}

	c.scheduleDelayedNonOwner(msg, peer, metadata, sender)
}

func (c *WAReplyMateChannel) noteOwnerActivity(chatID string) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return
	}
	c.mu.Lock()
	c.ownerLastActive[chatID] = time.Now()
	c.mu.Unlock()
}

func (c *WAReplyMateChannel) ownerRecentlyActive(chatID string) bool {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return false
	}
	window := time.Duration(c.cfg.OwnerActiveWindowSec) * time.Second
	c.mu.Lock()
	last, ok := c.ownerLastActive[chatID]
	c.mu.Unlock()
	if !ok {
		return false
	}
	return time.Since(last) < window
}

func (c *WAReplyMateChannel) cancelPendingForChat(chatID string) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return
	}
	c.mu.Lock()
	pending := c.pendingByChat[chatID]
	if pending != nil {
		delete(c.pendingByChat, chatID)
	}
	c.mu.Unlock()
	if pending != nil && pending.cancel != nil {
		pending.cancel()
	}
}

func (c *WAReplyMateChannel) scheduleDelayedNonOwner(
	msg dbMessage,
	peer bus.Peer,
	metadata map[string]string,
	sender bus.SenderInfo,
) {
	if c.ownerRecentlyActive(msg.chatID) {
		logger.InfoCF("wareplymate", "Skipping non-owner delayed schedule while owner is active", map[string]any{
			"chat":   msg.chatID,
			"sender": msg.senderID,
		})
		return
	}

	delay := time.Duration(c.cfg.NonOwnerDelaySeconds) * time.Second
	msgCtx, cancel := context.WithCancel(c.ctx)

	chatID := strings.TrimSpace(msg.chatID)
	entry := &pendingInbound{messageID: msg.messageID, cancel: cancel}

	c.mu.Lock()
	prev := c.pendingByChat[chatID]
	c.pendingByChat[chatID] = entry
	c.mu.Unlock()

	if prev != nil && prev.cancel != nil {
		prev.cancel()
	}

	logger.InfoCF("wareplymate", "Scheduled delayed non-owner message", map[string]any{
		"chat":         msg.chatID,
		"sender":       msg.senderID,
		"message_id":   msg.messageID,
		"delay_second": c.cfg.NonOwnerDelaySeconds,
	})

	go func(local dbMessage, localPeer bus.Peer, localMeta map[string]string, localSender bus.SenderInfo, expectedID string) {
		select {
		case <-msgCtx.Done():
			return
		case <-time.After(delay):
		}

		if c.ownerRecentlyActive(local.chatID) {
			logger.InfoCF("wareplymate", "Dropped delayed non-owner message because owner became active", map[string]any{
				"chat":       local.chatID,
				"sender":     local.senderID,
				"message_id": local.messageID,
			})
			return
		}

		c.mu.Lock()
		current := c.pendingByChat[chatID]
		if current == nil || current.messageID != expectedID {
			c.mu.Unlock()
			return
		}
		delete(c.pendingByChat, chatID)
		c.mu.Unlock()

		c.HandleMessage(c.ctx, localPeer, local.messageID, local.senderID, local.chatID, local.content, nil, localMeta, localSender)
	}(msg, peer, metadata, sender, msg.messageID)
}

func normalizeJID(jid string) string {
	jid = strings.TrimSpace(strings.ToLower(jid))
	if jid == "" {
		return ""
	}
	if idx := strings.Index(jid, ":"); idx >= 0 {
		jid = jid[:idx]
	}
	if idx := strings.Index(jid, "@"); idx >= 0 {
		jid = jid[:idx]
	}
	jid = strings.TrimSpace(jid)
	jid = strings.TrimLeft(jid, "+")
	jid = strings.NewReplacer(" ", "", "-", "", "_", "", "(", "", ")", "").Replace(jid)
	return strings.TrimSpace(jid)
}

func (c *WAReplyMateChannel) shouldHandleSelfMessage(content string) bool {
	prefix := strings.ToLower(strings.TrimSpace(c.cfg.SelfCommandPrefix))
	if prefix == "" {
		prefix = defaultSelfCommandPrefix
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(content)), prefix)
}

func (c *WAReplyMateChannel) stripSelfCommandPrefix(content string) string {
	prefix := strings.TrimSpace(c.cfg.SelfCommandPrefix)
	if prefix == "" {
		prefix = defaultSelfCommandPrefix
	}
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(strings.ToLower(trimmed), strings.ToLower(prefix)) {
		trimmed = strings.TrimSpace(trimmed[len(prefix):])
	}
	trimmed = strings.TrimLeft(trimmed, ":- ")
	return strings.TrimSpace(trimmed)
}

func (c *WAReplyMateChannel) rememberOutboundEcho(chatID, content string) {
	key := outboundEchoKey(chatID, content)
	if key == "" {
		return
	}
	now := time.Now()

	c.mu.Lock()
	for k, ts := range c.recentOutbound {
		if now.Sub(ts) > outboundEchoTTL {
			delete(c.recentOutbound, k)
		}
	}
	c.recentOutbound[key] = now
	c.mu.Unlock()
}

func (c *WAReplyMateChannel) isLikelyOutboundEcho(chatID, content string) bool {
	key := outboundEchoKey(chatID, content)
	if key == "" {
		return false
	}
	now := time.Now()

	c.mu.Lock()
	ts, ok := c.recentOutbound[key]
	if ok && now.Sub(ts) <= outboundEchoTTL {
		delete(c.recentOutbound, key)
		c.mu.Unlock()
		return true
	}
	for k, t := range c.recentOutbound {
		if now.Sub(t) > outboundEchoTTL {
			delete(c.recentOutbound, k)
		}
	}
	c.mu.Unlock()
	return false
}

func outboundEchoKey(chatID, content string) string {
	chat := strings.TrimSpace(strings.ToLower(chatID))
	text := strings.TrimSpace(content)
	if chat == "" || text == "" {
		return ""
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(chat))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(text))
	return fmt.Sprintf("%x", h.Sum64())
}

func shouldIgnoreSystemChat(chatID string) bool {
	chatID = strings.TrimSpace(strings.ToLower(chatID))
	if chatID == "" {
		return false
	}
	if chatID == "status@broadcast" {
		return true
	}
	if strings.HasSuffix(chatID, "@broadcast") {
		return true
	}
	if strings.HasSuffix(chatID, "@newsletter") {
		return true
	}
	return false
}
