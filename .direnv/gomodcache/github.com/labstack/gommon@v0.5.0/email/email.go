package email

import (
	"bytes"
	"crypto/tls"
	"html/template"
	"net"
	"net/mail"
	"net/smtp"
	"time"

	"github.com/labstack/gommon/random"
)

type (
	Email struct {
		Auth     smtp.Auth
		Header   map[string]string
		Template *template.Template
		// TLSConfig, when non-nil, is used for both implicit TLS (SMTPS)
		// and STARTTLS. Callers that need a custom root pool or a
		// specific ServerName should set this. The config is cloned per
		// dial, so callers can reuse a single value across sends; they
		// must not mutate it concurrently with an in-flight Send.
		TLSConfig *tls.Config
		// DialTimeout caps the TCP (and, for SMTPS, TLS) connect phase.
		// It does not bound the full SMTP conversation after the client
		// is returned. Zero means no caller-imposed timeout.
		DialTimeout time.Duration
		smtpAddress string
	}

	Message struct {
		ID          string  `json:"id"`
		From        string  `json:"from"`
		To          string  `json:"to"`
		CC          string  `json:"cc"`
		Subject     string  `json:"subject"`
		BodyText    string  `json:"body_text"`
		BodyHTML    string  `json:"body_html"`
		Inlines     []*File `json:"inlines"`
		Attachments []*File `json:"attachments"`
		buffer      *bytes.Buffer
		boundary    string
	}

	File struct {
		Name    string
		Type    string
		Content string
	}
)

func New(smtpAddress string) *Email {
	return &Email{
		smtpAddress: smtpAddress,
		Header:      map[string]string{},
	}
}

func (m *Message) writeHeader(key, value string) {
	m.buffer.WriteString(key)
	m.buffer.WriteString(": ")
	m.buffer.WriteString(value)
	m.buffer.WriteString("\r\n")
}

func (m *Message) writeBoundary() {
	m.buffer.WriteString("--")
	m.buffer.WriteString(m.boundary)
	m.buffer.WriteString("\r\n")
}

func (m *Message) writeText(content string, contentType string) {
	m.writeBoundary()
	m.writeHeader("Content-Type", contentType+"; charset=UTF-8")
	m.buffer.WriteString("\r\n")
	m.buffer.WriteString(content)
	m.buffer.WriteString("\r\n")
	m.buffer.WriteString("\r\n")
}

func (m *Message) writeFile(f *File, disposition string) {
	m.writeBoundary()
	m.writeHeader("Content-Type", f.Type+`; name="`+f.Name+`"`)
	m.writeHeader("Content-Disposition", disposition+`; filename="`+f.Name+`"`)
	m.writeHeader("Content-Transfer-Encoding", "base64")
	m.buffer.WriteString("\r\n")
	m.buffer.WriteString(f.Content)
	m.buffer.WriteString("\r\n")
	m.buffer.WriteString("\r\n")
}

func (e *Email) Send(m *Message) (err error) {
	// Message header
	m.buffer = bytes.NewBuffer(make([]byte, 256))
	m.buffer.Reset()
	m.boundary = random.String(16)
	m.writeHeader("MIME-Version", "1.0")
	m.writeHeader("Message-ID", m.ID)
	m.writeHeader("Date", time.Now().Format(time.RFC1123Z))
	m.writeHeader("From", m.From)
	m.writeHeader("To", m.To)
	if m.CC != "" {
		m.writeHeader("CC", m.CC)
	}
	if m.Subject != "" {
		m.writeHeader("Subject", m.Subject)
	}
	// Extra
	for k, v := range e.Header {
		m.writeHeader(k, v)
	}
	m.writeHeader("Content-Type", "multipart/mixed; boundary="+m.boundary)
	m.buffer.WriteString("\r\n")

	// Message body
	if m.BodyText != "" {
		m.writeText(m.BodyText, "text/plain")
	} else if m.BodyHTML != "" {
		m.writeText(m.BodyHTML, "text/html")
	} else {
		m.writeBoundary()
	}

	// Inlines/attachments
	for _, f := range m.Inlines {
		m.writeFile(f, "inline")
	}
	for _, f := range m.Attachments {
		m.writeFile(f, "attachment")
	}
	m.buffer.WriteString("--")
	m.buffer.WriteString(m.boundary)
	m.buffer.WriteString("--")

	// Dial. Port 465 is SMTPS (implicit TLS) per IANA and always uses
	// TLS. Other ports connect plaintext and opportunistically upgrade
	// to STARTTLS only if the server advertises it — if the server
	// doesn't, the connection stays in the clear. Operators that
	// require TLS must use port 465.
	c, err := e.dial()
	if err != nil {
		return
	}
	defer c.Quit()

	// Authenticate
	if e.Auth != nil {
		if err = c.Auth(e.Auth); err != nil {
			return
		}
	}

	// Send message
	from, err := mail.ParseAddress(m.From)
	if err != nil {
		return
	}
	if err = c.Mail(from.Address); err != nil {
		return
	}
	to, err := mail.ParseAddressList(m.To)
	if err != nil {
		return
	}
	for _, a := range to {
		if err = c.Rcpt(a.Address); err != nil {
			return
		}
	}
	wc, err := c.Data()
	if err != nil {
		return
	}
	defer wc.Close()
	_, err = m.buffer.WriteTo(wc)
	return
}

func (e *Email) dial() (*smtp.Client, error) {
	host, port, err := net.SplitHostPort(e.smtpAddress)
	if err != nil {
		return nil, err
	}

	// Always clone so we never mutate the caller's TLSConfig.
	var tlsConfig *tls.Config
	if e.TLSConfig == nil {
		tlsConfig = &tls.Config{ServerName: host}
	} else {
		tlsConfig = e.TLSConfig.Clone()
		if tlsConfig.ServerName == "" {
			tlsConfig.ServerName = host
		}
	}

	dialer := &net.Dialer{Timeout: e.DialTimeout}

	if port == "465" {
		conn, err := tls.DialWithDialer(dialer, "tcp", e.smtpAddress, tlsConfig)
		if err != nil {
			return nil, err
		}
		c, err := smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
			return nil, err
		}
		return c, nil
	}

	conn, err := dialer.Dial("tcp", e.smtpAddress)
	if err != nil {
		return nil, err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return nil, err
	}
	// Drive EHLO explicitly so we can surface its error. (*Client).Extension
	// triggers a lazy hello() and swallows its error, which would silently
	// treat a failed EHLO as "STARTTLS not advertised" and stay cleartext.
	if err := c.Hello("localhost"); err != nil {
		c.Close()
		return nil, err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(tlsConfig); err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}
