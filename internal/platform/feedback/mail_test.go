package feedback

import (
	"os"
	"strings"
	"testing"
)

func sampleWire() wireSubmit {
	return wireSubmit{
		Category:    Bug,
		Body:        "远程页面打不开，设置分区报错。\n第二行在这里。",
		DisplayName: "阿宁",
		Contact:     "someone@example.com",
		Env: Env{
			Version: "v1.1.0", Channel: "stable", Surface: "studio",
			OS: "android", OSVersion: "16", Arch: "arm64", Locale: "zh",
			ProviderKind: "deepseek",
		},
	}
}

func TestAMailedReportCarriesEverythingTheFormHeld(t *testing.T) {
	wire := sampleWire()
	body := mailBody(wire, "FB-ABCD-EFGH")
	for _, want := range []string{
		"FB-ABCD-EFGH", "缺陷", "阿宁", "someone@example.com",
		"远程页面打不开", "第二行在这里",
		"v1.1.0", "android", "arm64", "deepseek",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("mailed body lacks %q:\n%s", want, body)
		}
	}
}

// A report with no contact still goes: the recipient is the maintainer, so the
// reporter's own address is optional and its absence must not read as a hole in
// the message.
func TestAMailedReportWithoutAContactStillReadsWhole(t *testing.T) {
	wire := sampleWire()
	wire.Contact = ""
	body := mailBody(wire, "FB-ABCD-EFGH")
	if strings.Contains(body, "联系方式") {
		t.Errorf("absent contact printed a label with nothing after it:\n%s", body)
	}
	if !strings.Contains(body, "远程页面打不开") {
		t.Error("the report's own words are missing")
	}
}

func TestTheSubjectNamesTheKindAndTheReceipt(t *testing.T) {
	subject := mailSubject(sampleWire(), "FB-ABCD-EFGH")
	if !strings.Contains(subject, "缺陷") || !strings.Contains(subject, "FB-ABCD-EFGH") {
		t.Errorf("subject = %q, want the kind and the receipt", subject)
	}
	// The first line only, and short: a subject is one line in a list, and a
	// report whose first line is a paragraph must not become one.
	if strings.Contains(subject, "第二行") {
		t.Errorf("subject took a second line: %q", subject)
	}
}

func TestALongFirstLineIsCutToAFixedWidth(t *testing.T) {
	wire := sampleWire()
	wire.Body = strings.Repeat("很", 200)
	subject := mailSubject(wire, "FB-ABCD-EFGH")
	if !strings.HasSuffix(subject, "…") {
		t.Errorf("subject = %q, want it truncated with an ellipsis", subject)
	}
	if r := []rune(subject); len(r) > 80 {
		t.Errorf("subject is %d runes; a mailbox shows about 78", len(r))
	}
}

// A Chinese subject is not ASCII, and a bare UTF-8 header is what a mail client
// shows as mojibake. The encoded form is the check: it must be ASCII, and it
// must carry the words.
func TestAChineseSubjectIsEncodedForTheHeader(t *testing.T) {
	message := mailMessage(MailConfig{User: "me@example.com", To: "you@example.com"}, mailSubject(sampleWire(), "FB-ABCD-EFGH"), "正文")
	head, body, ok := strings.Cut(message, "\r\n\r\n")
	if !ok {
		t.Fatalf("no header/body split in:\n%s", message)
	}
	for _, line := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(line, "Subject: ") {
			value := strings.TrimPrefix(line, "Subject: ")
			if !strings.HasPrefix(value, "=?utf-8?") {
				t.Errorf("subject header is not encoded: %q", value)
			}
			if strings.ContainsAny(value, "反馈") {
				t.Errorf("raw Chinese in a header: %q", value)
			}
		}
	}
	// Every line of a body is CRLF-terminated: a relay that counts line length
	// rewrites bare newlines for you or refuses the message.
	if strings.Contains(strings.ReplaceAll(body, "\r\n", ""), "\n") {
		t.Errorf("body has a bare newline:\n%q", body)
	}
	if !strings.Contains(body, "正文") {
		t.Error("body text is missing")
	}
}

func TestScreenshotsRideAlongAsParts(t *testing.T) {
	cfg := MailConfig{User: "me@example.com", To: "you@example.com"}
	attachments := []wireAttachment{
		{Name: "shot.png", ContentType: "image/png", DataBase64: strings.Repeat("A", 200)},
	}
	message := mailWithAttachments(cfg, "标题", "正文", attachments)
	if !strings.Contains(message, "multipart/mixed") {
		t.Error("a report with a picture is not multipart")
	}
	if !strings.Contains(message, "Content-Transfer-Encoding: base64") {
		t.Error("the picture is not base64")
	}
	if !strings.Contains(message, "shot.png") {
		t.Error("the picture's name is missing")
	}
	// RFC 2045 asks for 76-column lines, and a relay that counts line length
	// rejects a payload sent as one long line.
	for _, line := range strings.Split(message, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("a line is %d bytes; SMTP allows 998", len(line))
		}
	}
	if !strings.Contains(message, "正文") {
		t.Error("the report's text is missing from the multipart message")
	}
}

// Without a picture there is nothing to frame, and a multipart body around one
// part is what a client shows as an attachment instead of a message.
func TestAReportWithoutScreenshotsIsPlain(t *testing.T) {
	message := mailWithAttachments(MailConfig{User: "me@example.com", To: "you@example.com"}, "标题", "正文", nil)
	if strings.Contains(message, "multipart") {
		t.Errorf("a text-only report was framed as multipart:\n%s", message)
	}
}

func TestLocalReceiptsAreShapedLikeTheServices(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		got, err := localReceipt()
		if err != nil {
			t.Fatal(err)
		}
		if !receiptPattern.MatchString(got) {
			t.Fatalf("receipt %q does not match the pattern the frontends validate with", got)
		}
		// The alphabet drops I, O, 0 and 1, which is what keeps a receipt
		// something a person can read aloud without a second guess.
		for _, bad := range []string{"I", "O", "0", "1"} {
			if strings.Contains(got, bad) {
				t.Fatalf("receipt %q contains %q, which reads as another character", got, bad)
			}
		}
		if seen[got] {
			t.Fatalf("receipt %q repeated", got)
		}
		seen[got] = true
	}
}

// mailFromEnv is the switch between the two delivery paths. Every test in this
// package runs with it off, which is what keeps them the upstream ones.
func TestTheMailboxIsOffUnlessTheShellNamedOne(t *testing.T) {
	for _, key := range []string{mailRelayEnv, mailUserEnv, mailPassEnv, mailToEnv} {
		t.Setenv(key, "")
	}
	if got := mailFromEnv(); got != nil {
		t.Fatalf("mailFromEnv = %+v with nothing set, want nil", got)
	}
	t.Setenv(mailRelayEnv, "smtp.example.com:465")
	t.Setenv(mailUserEnv, "me@example.com")
	t.Setenv(mailToEnv, "you@example.com")
	if got := mailFromEnv(); got == nil {
		t.Fatal("mailFromEnv = nil with a relay, a user and a recipient set")
	}
	// A relay with no recipient has nowhere to deliver, so it is not a mailbox.
	t.Setenv(mailToEnv, "")
	if got := mailFromEnv(); got != nil {
		t.Fatalf("mailFromEnv = %+v without a recipient, want nil", got)
	}
}

// A real send, off unless a relay is named. It is what makes the rest of this
// file more than a description of a message: the header encoding, the CRLF
// framing and the auth handshake are all judged by an actual server.
func TestMailReachesARealRelay(t *testing.T) {
	cfg := mailFromEnv()
	if cfg == nil {
		t.Skip("no REASONIX_FEEDBACK_MAIL_* set; skipping the live relay check")
	}
	receipt, err := localReceipt()
	if err != nil {
		t.Fatal(err)
	}
	wire := sampleWire()
	wire.Body = "这是 1.1.0 的手机端反馈测试邮件，收到即说明投递链路通了。"
	message := mailWithAttachments(*cfg, mailSubject(wire, receipt), mailBody(wire, receipt), nil)
	if err := sendMail(*cfg, message); err != nil {
		t.Fatalf("sendMail: %v", err)
	}
	if os.Getenv("REASONIX_FEEDBACK_MAIL_ECHO") != "" {
		t.Logf("sent %s", receipt)
	}
}
