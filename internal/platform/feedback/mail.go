// mail.go — delivering a report as email instead of to the service.
//
// The endpoint at `crash.reasonix.io` belongs to the upstream project: a report
// filed there is triaged by people who do not ship this build, and a phone
// build's reports would land where nobody who can fix them is looking. So a
// build may name an SMTP relay and a recipient, and every report is mailed
// there instead of posted.
//
// Nothing here runs unless the relay and the recipient are both set, so an
// ordinary build — and every test in this package — is the upstream one
// unchanged. The credentials are read from the environment rather than from the
// config file: a password written beside the reports it protects is one more
// copy of it on disk, and the shell that launches this kernel already passes
// the values it needs that way.
package feedback

import (
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

// Where a build that mails its reports says so. All four are needed; any one
// missing leaves the service path exactly as upstream wrote it.
const (
	mailRelayEnv = "REASONIX_FEEDBACK_MAIL_SMTP"
	mailUserEnv  = "REASONIX_FEEDBACK_MAIL_USER"
	mailPassEnv  = "REASONIX_FEEDBACK_MAIL_PASS"
	mailToEnv    = "REASONIX_FEEDBACK_MAIL_TO"
)

// MailConfig is where a report goes when it is delivered as mail.
type MailConfig struct {
	// Addr is the relay's host:port. Port 465 is implicit TLS — encrypted from
	// the first byte — and every other port is upgraded with STARTTLS before
	// the password is offered.
	Addr string
	User string
	Pass string
	// To is the recipient. It is the maintainer's own address, not the
	// reporter's: the form's contact field is optional, and a report with no
	// address on it must still arrive.
	To string
}

// mailFromEnv builds the configuration a shell passed in, or nil.
func mailFromEnv() *MailConfig {
	addr := strings.TrimSpace(os.Getenv(mailRelayEnv))
	to := strings.TrimSpace(os.Getenv(mailToEnv))
	if addr == "" || to == "" {
		return nil
	}
	user := strings.TrimSpace(os.Getenv(mailUserEnv))
	if user == "" {
		return nil
	}
	return &MailConfig{Addr: addr, User: user, Pass: os.Getenv(mailPassEnv), To: to}
}

// errMail is a relay that refused or could not be reached. It is reported as
// the service being unreachable, which is what it is: the report has nowhere to
// go, and trying again is the right next step.
var errMail = errors.New("feedback: mail relay")

// sendMail delivers one already-built message.
//
// The message is built by the caller rather than here, because a report with
// screenshots is a multipart one and a report without them is not: passing the
// parts in and assembling the body at the last step would be the same decision
// made twice, and the second one is where a picture gets dropped.
func sendMail(cfg MailConfig, message string) error {
	host, portText, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return fmt.Errorf("%w: %v", errMail, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return fmt.Errorf("%w: %v", errMail, err)
	}

	if port == 465 {
		conn, err := tls.Dial("tcp", cfg.Addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err != nil {
			return fmt.Errorf("%w: %v", errMail, err)
		}
		defer conn.Close()
		// Implicit TLS: the connection is already encrypted, so the greeting is
		// read straight off it and no upgrade follows.
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			return fmt.Errorf("%w: %v", errMail, err)
		}
		return deliver(client, host, cfg, message)
	}

	conn, err := smtp.Dial(cfg.Addr)
	if err != nil {
		return fmt.Errorf("%w: %v", errMail, err)
	}
	defer conn.Close()
	// Upgraded before the password is offered. `smtp.PlainAuth` refuses to send
	// one over a connection it cannot see is encrypted, which is the right
	// answer and the reason this is not optional.
	if ok, _ := conn.Extension("STARTTLS"); ok {
		if err := conn.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("%w: %v", errMail, err)
		}
	}
	return deliver(conn, host, cfg, message)
}

// deliver runs the four SMTP verbs a single message needs.
func deliver(conn *smtp.Client, host string, cfg MailConfig, message string) error {
	if err := conn.Auth(smtp.PlainAuth("", cfg.User, cfg.Pass, host)); err != nil {
		return fmt.Errorf("%w: %v", errMail, err)
	}
	if err := conn.Mail(cfg.User); err != nil {
		return fmt.Errorf("%w: %v", errMail, err)
	}
	if err := conn.Rcpt(cfg.To); err != nil {
		return fmt.Errorf("%w: %v", errMail, err)
	}
	w, err := conn.Data()
	if err != nil {
		return fmt.Errorf("%w: %v", errMail, err)
	}
	if _, err := w.Write([]byte(message)); err != nil {
		return fmt.Errorf("%w: %v", errMail, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("%w: %v", errMail, err)
	}
	return conn.Quit()
}

// mailHeaders is the four lines every message starts with. A Chinese subject is
// not ASCII, and a bare UTF-8 header is what a mail client shows as mojibake —
// so it is encoded, and the receipt rides in it so a reply can quote one
// unambiguous thing.
func mailHeaders(cfg MailConfig, subject string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", cfg.User)
	fmt.Fprintf(&b, "To: %s\r\n", cfg.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	return b.String()
}

// crlfBody frames a body the way SMTP requires. A body written with bare
// newlines is what a relay rewrites for you or refuses outright.
func crlfBody(body string) string {
	return strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
}

// mailMessage is the one message a report becomes.
func mailMessage(cfg MailConfig, subject, body string) string {
	return mailHeaders(cfg, subject) +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" +
		crlfBody(body)
}

// mailSubject is the one line a mailbox shows.
func mailSubject(wire wireSubmit, receipt string) string {
	head := firstLine(wire.Body)
	if r := []rune(head); len(r) > 40 {
		head = string(r[:40]) + "…"
	}
	return fmt.Sprintf("[Reasonix 反馈] %s %s %s", categoryLabel(wire.Category), receipt, head)
}

func firstLine(body string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	return strings.TrimSpace(line)
}

// mailBody is everything the report carried, in the order a person reads it:
// what was said, who said it, and what machine it came from.
func mailBody(wire wireSubmit, receipt string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "回执号：%s\n", receipt)
	fmt.Fprintf(&b, "类型：%s\n", categoryLabel(wire.Category))
	if wire.DisplayName != "" {
		fmt.Fprintf(&b, "称呼：%s\n", wire.DisplayName)
	}
	// Optional by design: a report with no address on it still arrives, because
	// the recipient is the maintainer rather than the reporter.
	if wire.Contact != "" {
		fmt.Fprintf(&b, "联系方式：%s\n", wire.Contact)
	}
	b.WriteString("\n--- 内容 ---\n\n")
	b.WriteString(wire.Body)
	b.WriteString("\n\n--- 环境 ---\n")
	fmt.Fprintf(&b, "版本：%s（%s）\n", wire.Env.Version, wire.Env.Channel)
	fmt.Fprintf(&b, "界面：%s\n", wire.Env.Surface)
	fmt.Fprintf(&b, "系统：%s %s / %s\n", wire.Env.OS, wire.Env.OSVersion, wire.Env.Arch)
	fmt.Fprintf(&b, "语言：%s\n", wire.Env.Locale)
	if wire.Env.ProviderKind != "" {
		fmt.Fprintf(&b, "模型来源：%s\n", wire.Env.ProviderKind)
	}
	if n := len(wire.Attachments); n > 0 {
		fmt.Fprintf(&b, "\n随附 %d 张截图。\n", n)
	}
	return b.String()
}

// mailWithAttachments frames the text and every screenshot as one message.
//
// A report that carried a picture is not the same report without it — a layout
// that is wrong is a picture before it is a sentence — so the images ride along
// rather than being counted and dropped. Base64 in a MIME part is what every
// mail client already reads; the alternative is telling the person to send
// their screenshots somewhere else.
func mailWithAttachments(cfg MailConfig, subject, body string, attachments []wireAttachment) string {
	if len(attachments) == 0 {
		return mailMessage(cfg, subject, body)
	}
	boundary, err := mailBoundary()
	if err != nil {
		// No boundary is no message; the text alone still carries the report,
		// and losing the pictures is better than losing the words.
		return mailMessage(cfg, subject, body)
	}
	var b strings.Builder
	b.WriteString(mailHeaders(cfg, subject))
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=%q\r\n", boundary)
	b.WriteString("\r\n")
	// The text first, so a client that shows only the head still shows it.
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(crlfBody(body))
	b.WriteString("\r\n")
	for _, a := range attachments {
		kind := a.ContentType
		if kind == "" {
			kind = "application/octet-stream"
		}
		fmt.Fprintf(&b, "--%s\r\n", boundary)
		fmt.Fprintf(&b, "Content-Type: %s; name=%s\r\n", kind, mime.QEncoding.Encode("utf-8", a.Name))
		b.WriteString("Content-Transfer-Encoding: base64\r\n")
		fmt.Fprintf(&b, "Content-Disposition: attachment; filename=%s\r\n\r\n", mime.QEncoding.Encode("utf-8", a.Name))
		b.WriteString(wrapBase64(a.DataBase64))
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.String()
}

// wrapBase64 breaks the payload at 76 columns, which is what RFC 2045 asks for
// and what a relay that counts line length will otherwise reject.
func wrapBase64(data string) string {
	var b strings.Builder
	for len(data) > 76 {
		b.WriteString(data[:76])
		b.WriteString("\r\n")
		data = data[76:]
	}
	b.WriteString(data)
	return b.String()
}

func mailBoundary() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return fmt.Sprintf("=--reasonix-%x", raw), nil
}

// localReceipt mints the acknowledgement a mailed report gets.
//
// The service issues one; a relay does not, and the form is built around there
// being something to write down and quote back. This is the same shape as the
// service's — `FB-XXXX-XXXX`, from an alphabet without the letters and digits
// that read as each other — so the receipt the person keeps looks like every
// other one this product has ever shown them.
func localReceipt() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("FB-")
	for i, v := range raw {
		if i == 4 {
			b.WriteByte('-')
		}
		b.WriteByte(alphabet[int(v)%len(alphabet)])
	}
	return b.String(), nil
}

// categoryLabel is the report's own kind, in the words the mail is read with.
func categoryLabel(c Category) string {
	switch c {
	case Bug:
		return "缺陷"
	case Idea:
		return "建议"
	case Question:
		return "疑问"
	default:
		return "其他"
	}
}
