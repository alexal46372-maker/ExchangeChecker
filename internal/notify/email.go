package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 13b (CHK-SVC-08 / CHK-IPC-16): email notifications. Alerts are
// queued asynchronously (never block the monitor loop). SMTP via stdlib
// net/smtp + crypto/tls; Dial is injectable for tests without live SMTP.

// EmailPasswordStore loads the SMTP password (config.Store).
type EmailPasswordStore interface {
	LoadEmailPassword() (string, error)
}

// SMTPDialer dials the SMTP server (tests inject a fake).
type SMTPDialer func(ctx context.Context, network, address string) (net.Conn, error)

// EmailOptions of NewEmail; zero = production defaults.
type EmailOptions struct {
	Store EmailPasswordStore
	Dial  SMTPDialer // nil = net.Dialer{Timeout:15s}.DialContext
	Lang  string
	Log   Logger
	Now   func() time.Time
	Loc   *time.Location
	// SendHook, when set, replaces the real SMTP send (unit tests).
	SendHook func(ctx context.Context, host string, port int, tlsMode, from, password, to, subject, body string) error
	// TLS, when set, is used for StartTLS / implicit TLS instead of the
	// default (ServerName=host, MinVersion TLS1.2). Tests inject a config
	// with RootCAs / InsecureSkipVerify for a fake listener.
	TLS *tls.Config
}

type emailItem struct {
	to, subject, body string
	attempts          int
	next              time.Time
}

// EmailNotifier implements alert.Sink + ipc.EmailTester.
type EmailNotifier struct {
	o        EmailOptions
	mu       sync.Mutex
	settings model.EmailSettings
	password string
	queue    []emailItem
	status   model.ViberStatus
	lastTest time.Time
	wake     chan struct{}
	stop     chan struct{}
	wg       sync.WaitGroup
}

// NewEmail builds an EmailNotifier. Call Close to stop the worker.
func NewEmail(o EmailOptions) *EmailNotifier {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Loc == nil {
		o.Loc = time.Local
	}
	if o.Log == nil {
		o.Log = nopLog{}
	}
	if o.Dial == nil {
		d := &net.Dialer{Timeout: 15 * time.Second}
		o.Dial = d.DialContext
	}
	n := &EmailNotifier{
		o: o, settings: model.DefaultEmailSettings(),
		wake: make(chan struct{}, 1), stop: make(chan struct{}),
	}
	n.settings.Enabled = false
	n.ReloadPassword()
	n.wg.Add(1)
	go n.loop()
	return n
}

// Close stops the worker.
func (n *EmailNotifier) Close() {
	select {
	case <-n.stop:
	default:
		close(n.stop)
	}
	n.wg.Wait()
}

// ReloadPassword rereads email.dpapi.
func (n *EmailNotifier) ReloadPassword() {
	var p string
	if n.o.Store != nil {
		var err error
		p, err = n.o.Store.LoadEmailPassword()
		if err != nil {
			n.o.Log.Printf("email: password: %v (email off)", err)
			p = ""
		}
	}
	n.mu.Lock()
	n.password = p
	n.mu.Unlock()
}

// Status is settings.email.status (views).
func (n *EmailNotifier) Status() *model.ViberStatus {
	n.mu.Lock()
	defer n.mu.Unlock()
	s := n.status
	s.Queued = len(n.queue)
	return &s
}

// Snapshot feeds settings.email from the monitor.
func (n *EmailNotifier) Snapshot(s model.StatusSnapshot) {
	n.mu.Lock()
	if s.Settings != nil && s.Settings.Email != nil {
		e := s.Settings.Email.Stored()
		e.Recipients = append([]model.EmailRecipient{}, e.Recipients...)
		e.Events = append([]string{}, e.Events...)
		e.Schedule.Days = append([]int{}, e.Schedule.Days...)
		if !sameEmailRecipients(n.settings.Recipients, e.Recipients) || !e.Enabled {
			n.queue = nil
		}
		n.settings = e
	}
	n.mu.Unlock()
	n.kick()
}

func sameEmailRecipients(a, b []model.EmailRecipient) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Addr != b[i].Addr {
			return false
		}
	}
	return true
}

// Alert enqueues a mail when email is enabled, the event matches and the
// schedule window is open. Never blocks the caller beyond a short lock.
func (n *EmailNotifier) Alert(e model.AlertEvent) {
	kind := model.ViberEventOf(e)
	if kind == "" {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.settings.Enabled || !n.settings.HasEvent(kind) {
		return
	}
	if model.NeedsSMTPAuth(n.settings.TLSMode) && n.password == "" {
		return
	}
	now := n.o.Now().In(n.o.Loc)
	if !n.settings.InWindow(now) {
		n.status.Dropped++
		return
	}
	subj, body := EmailSubjectBody(e, n.o.Lang)
	for _, r := range n.settings.Recipients {
		if len(n.queue) >= MaxQueue {
			n.status.Dropped++
			continue
		}
		n.queue = append(n.queue, emailItem{to: r.Addr, subject: subj, body: body, next: now})
	}
	n.kick()
}

// EmailSubjectBody renders an alert for email (localized, no raw SQL).
func EmailSubjectBody(e model.AlertEvent, lang string) (subject, body string) {
	body = EventText(e, lang)
	if lang == model.LangEN {
		subject = "ExchangeChecker: " + e.Kind
	} else {
		subject = "ExchangeChecker: " + e.Kind
	}
	return subject, body
}

func (n *EmailNotifier) kick() {
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

func (n *EmailNotifier) loop() {
	defer n.wg.Done()
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-n.stop:
			return
		case <-n.wake:
			n.drain()
		case <-t.C:
			n.drain()
		}
	}
}

func (n *EmailNotifier) drain() {
	for {
		n.mu.Lock()
		if len(n.queue) == 0 || !n.settings.Enabled {
			n.mu.Unlock()
			return
		}
		if model.NeedsSMTPAuth(n.settings.TLSMode) && n.password == "" {
			n.mu.Unlock()
			return
		}
		now := n.o.Now()
		idx := -1
		for i, it := range n.queue {
			if !it.next.After(now) {
				idx = i
				break
			}
		}
		if idx < 0 {
			n.mu.Unlock()
			return
		}
		it := n.queue[idx]
		host, port, tlsMode, from := n.settings.Host, n.settings.Port, n.settings.TLSMode, n.settings.From
		pass := n.password
		n.queue = append(n.queue[:idx], n.queue[idx+1:]...)
		n.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := n.sendMail(ctx, host, port, tlsMode, from, pass, it.to, it.subject, it.body)
		cancel()

		n.mu.Lock()
		if err != nil {
			n.status.LastErrorKey = emailErrKey(err)
			nowt := n.o.Now()
			n.status.LastErrorAt = &nowt
			it.attempts++
			if it.attempts < MaxAttempts {
				bo := Backoff[it.attempts-1]
				if it.attempts-1 >= len(Backoff) {
					bo = Backoff[len(Backoff)-1]
				}
				it.next = n.o.Now().Add(bo)
				n.queue = append(n.queue, it)
			} else {
				n.o.Log.Printf("email: give up to %s after %d attempts", maskAddr(it.to), it.attempts)
			}
		} else {
			n.status.Sent++
			nowt := n.o.Now()
			n.status.LastOKAt = &nowt
			n.status.LastErrorKey = ""
		}
		n.mu.Unlock()
	}
}

func maskAddr(a string) string {
	if i := strings.IndexByte(a, '@'); i > 1 {
		return a[:1] + "***" + a[i:]
	}
	return "***"
}

func (n *EmailNotifier) sendMail(ctx context.Context, host string, port int, tlsMode, from, password, to, subject, body string) error {
	if n.o.SendHook != nil {
		return n.o.SendHook(ctx, host, port, tlsMode, from, password, to, subject, body)
	}
	return SendSMTP(ctx, n.o.Dial, n.o.TLS, host, port, tlsMode, from, password, to, subject, body)
}

// SendSMTP dials and sends one message via stdlib net/smtp + crypto/tls.
// tlsMode none: no TLS and no AUTH (password ignored; IP-allowlist relay).
// starttls / tls: TLS then AUTH with username = From when password != "".
func SendSMTP(ctx context.Context, dial SMTPDialer, tlsCfg *tls.Config, host string, port int, tlsMode, from, password, to, subject, body string) error {
	if dial == nil {
		d := &net.Dialer{Timeout: 15 * time.Second}
		dial = d.DialContext
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := dial(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("%s: %w", model.MsgEmailNetwork, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	cfg := tlsCfg
	if cfg == nil {
		cfg = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	} else {
		c := cfg.Clone()
		if c.ServerName == "" {
			c.ServerName = host
		}
		if c.MinVersion == 0 {
			c.MinVersion = tls.VersionTLS12
		}
		cfg = c
	}

	var client *smtp.Client
	switch tlsMode {
	case model.EmailTLSImplicit, "ssl":
		tlsConn := tls.Client(conn, cfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("%s: %w", model.MsgEmailTLS, err)
		}
		client, err = smtp.NewClient(tlsConn, host)
	default:
		client, err = smtp.NewClient(conn, host)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", model.MsgEmailNetwork, err)
	}
	defer client.Close()

	if tlsMode == model.EmailTLSStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s: STARTTLS not offered", model.MsgEmailTLS)
		}
		if err := client.StartTLS(cfg); err != nil {
			return fmt.Errorf("%s: %w", model.MsgEmailTLS, err)
		}
	}
	// none: never AUTH (even if a password was stored / passed).
	// starttls/tls: AUTH username is the From address.
	if model.NeedsSMTPAuth(tlsMode) && password != "" {
		auth := smtp.PlainAuth("", from, password, host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("%s: %w", model.MsgEmailAuthFailed, err)
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("%s: %w", model.MsgEmailFailed, err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("%s: %w", model.MsgEmailInvalidAddr, err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("%s: %w", model.MsgEmailFailed, err)
	}
	msg := buildMIME(from, to, subject, body)
	if _, err := w.Write([]byte(msg)); err != nil {
		_ = w.Close()
		return fmt.Errorf("%s: %w", model.MsgEmailFailed, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("%s: %w", model.MsgEmailFailed, err)
	}
	_ = client.Quit()
	return nil
}

func buildMIME(from, to, subject, body string) string {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}

// EmailTest implements ipc.EmailTester (CHK-IPC-16): rate-limited like
// viber_test; ignores enabled/schedule; recipientId and/or addr.
func (n *EmailNotifier) EmailTest(ctx context.Context, recipientID, addr, smtpPassword string) (model.EmailTestResult, error) {
	n.mu.Lock()
	now := n.o.Now()
	if !n.lastTest.IsZero() && now.Sub(n.lastTest) < model.EmailTestInterval {
		wait := model.EmailTestInterval - now.Sub(n.lastTest)
		n.mu.Unlock()
		return model.EmailTestResult{}, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgEmailRateLimited,
			Message: "email: one test per 10 s", RetryAfterMs: int64(wait / time.Millisecond)}
	}
	pass := smtpPassword
	if pass == "" {
		pass = n.password
	}
	settings := n.settings
	n.mu.Unlock()

	if smtpPassword != "" && !model.ValidSMTPPassword(smtpPassword) {
		return model.EmailTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgEmailInvalid, Message: "email: invalid smtpPassword"}
	}
	if recipientID != "" && !model.ValidEmailID(recipientID) {
		return model.EmailTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgEmailInvalid, Message: "email: invalid recipientId"}
	}
	if addr != "" && !model.ValidEmailAddr(addr) {
		return model.EmailTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgEmailInvalidAddr, Message: "email: invalid addr"}
	}
	if model.NeedsSMTPAuth(settings.TLSMode) && pass == "" {
		return model.EmailTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgEmailNoPassword, Message: "email: no password"}
	}
	if !model.NeedsSMTPAuth(settings.TLSMode) {
		pass = "" // none: ignore stored / request password; no AUTH
	}
	if strings.TrimSpace(settings.Host) == "" {
		return model.EmailTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgEmailNoHost, Message: "email: no host"}
	}
	if strings.TrimSpace(settings.From) == "" {
		return model.EmailTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgEmailNoFrom, Message: "email: no from"}
	}

	type target struct{ id, addr string }
	var targets []target
	if addr != "" {
		targets = append(targets, target{addr: strings.TrimSpace(addr)})
	}
	if recipientID != "" {
		found := false
		for _, r := range settings.Recipients {
			if r.ID == recipientID {
				targets = append(targets, target{id: r.ID, addr: r.Addr})
				found = true
				break
			}
		}
		if !found {
			return model.EmailTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgEmailInvalidAddr,
				Message: "email: recipientId not found", Args: []string{"recipientId", model.ValRequired}}
		}
	}
	if len(targets) == 0 {
		for _, r := range settings.Recipients {
			targets = append(targets, target{id: r.ID, addr: r.Addr})
		}
	}
	if len(targets) == 0 {
		return model.EmailTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgEmailNoRecipients, Message: "email: no recipients"}
	}

	n.mu.Lock()
	n.lastTest = now
	n.mu.Unlock()

	subj := "ExchangeChecker: test"
	body := "ExchangeChecker: тестовое сообщение"
	if n.o.Lang == model.LangEN {
		body = "ExchangeChecker: test message"
	}
	res := model.EmailTestResult{OK: true, Results: []model.EmailTestItem{}}
	for _, t := range targets {
		if ctx.Err() != nil {
			key := model.MsgEmailCanceled
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				key = model.MsgEmailTimeout
			}
			res.Results = append(res.Results, model.EmailTestItem{ID: t.id, Addr: t.addr, OK: false, ErrorKey: key})
			res.OK = false
			continue
		}
		err := n.sendMail(ctx, settings.Host, settings.Port, settings.TLSMode, settings.From, pass, t.addr, subj, body)
		item := model.EmailTestItem{ID: t.id, Addr: t.addr, OK: err == nil}
		if err != nil {
			item.ErrorKey = emailErrKey(err)
			res.OK = false
		}
		res.Results = append(res.Results, item)
	}
	return res, nil
}

func emailErrKey(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	for _, k := range []string{
		model.MsgEmailAuthFailed, model.MsgEmailTLS, model.MsgEmailNetwork,
		model.MsgEmailInvalidAddr, model.MsgEmailFailed, model.MsgEmailTimeout, model.MsgEmailCanceled,
	} {
		if strings.HasPrefix(s, k) {
			return k
		}
	}
	return model.MsgEmailFailed
}
