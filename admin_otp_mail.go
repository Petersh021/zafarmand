package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

const (
	// adminOTPMailTimeout bounds the complete SMTP conversation, including TLS
	// negotiation, authentication, and delivery of the small fixed-format body.
	adminOTPMailTimeout = 10 * time.Second
	// adminOTPCodeLength mirrors the four optical cells in the approved reference.
	adminOTPCodeLength = 4
	// adminOTPMultipartBoundary is fixed because every variable body value is
	// constrained to ASCII digits or a server-formatted UTC timestamp.
	adminOTPMultipartBoundary = "zafarmand-admin-verification"
)

var (
	// errAdminOTPMailerConfigInvalid prevents a partially configured transport
	// from silently accepting verification requests it cannot deliver.
	errAdminOTPMailerConfigInvalid = errors.New("admin OTP mail configuration is invalid")
	// errAdminOTPMailInvalid keeps recipient and code values out of returned
	// diagnostics while still separating caller input from transport failures.
	errAdminOTPMailInvalid = errors.New("admin OTP mail is invalid")
	// errAdminOTPMailDeliveryFailed is deliberately account-neutral and contains
	// no SMTP response, address, credential, or verification code.
	errAdminOTPMailDeliveryFailed = errors.New("admin OTP mail delivery failed")
)

// adminOTPMailDeliveryStage is a closed, non-secret description of the SMTP
// operation that failed. Provider responses are deliberately never retained.
type adminOTPMailDeliveryStage uint8

const (
	adminOTPMailDeliveryStageConnect adminOTPMailDeliveryStage = iota + 1
	adminOTPMailDeliveryStageTLS
	adminOTPMailDeliveryStageAuthenticate
	adminOTPMailDeliveryStageSender
	adminOTPMailDeliveryStageRecipient
	adminOTPMailDeliveryStageMessage
)

// adminOTPMailDeliveryError preserves only a fixed stage. Error and Unwrap
// remain generic so accidental formatting cannot reveal a provider response,
// mailbox, credential, or verification code.
type adminOTPMailDeliveryError struct {
	stage adminOTPMailDeliveryStage
}

func (deliveryError *adminOTPMailDeliveryError) Error() string {
	return errAdminOTPMailDeliveryFailed.Error()
}

func (deliveryError *adminOTPMailDeliveryError) Unwrap() error {
	return errAdminOTPMailDeliveryFailed
}

func newAdminOTPMailDeliveryError(
	stage adminOTPMailDeliveryStage,
) error {
	return &adminOTPMailDeliveryError{stage: stage}
}

// adminOTPMailDeliveryStageLabel returns only one reviewed log value. Unknown
// mailer implementations cannot inject their error text into operational logs.
func adminOTPMailDeliveryStageLabel(err error) string {
	var deliveryError *adminOTPMailDeliveryError
	if !errors.As(err, &deliveryError) || deliveryError == nil {
		return "unknown"
	}

	switch deliveryError.stage {
	case adminOTPMailDeliveryStageConnect:
		return "connect"
	case adminOTPMailDeliveryStageTLS:
		return "tls"
	case adminOTPMailDeliveryStageAuthenticate:
		return "authenticate"
	case adminOTPMailDeliveryStageSender:
		return "sender"
	case adminOTPMailDeliveryStageRecipient:
		return "recipient"
	case adminOTPMailDeliveryStageMessage:
		return "message"
	default:
		return "unknown"
	}
}

// adminOTPMailer is the narrow outbound boundary used by the OTP workflow.
// Tests provide a recorder, while production supplies the TLS-only SMTP client.
type adminOTPMailer interface {
	SendOTP(context.Context, string, string, time.Time) error
}

// adminSMTPConfig contains only validated delivery settings. Its password must
// originate from process secrets and must never be rendered, logged, or stored.
type adminSMTPConfig struct {
	Address  string
	Host     string
	Username string
	Password string
	From     string
	Timeout  time.Duration
}

// smtpAdminOTPMailer delivers one multipart text/HTML code message over a
// mandatory STARTTLS connection. Both alternatives avoid remote images and
// links, so the message remains useful when external content is blocked.
type smtpAdminOTPMailer struct {
	address  string
	host     string
	username string
	password string
	from     *mail.Address
	timeout  time.Duration
}

// newSMTPAdminOTPMailer validates non-secret syntax before the server starts.
func newSMTPAdminOTPMailer(config adminSMTPConfig) (*smtpAdminOTPMailer, error) {
	address := strings.TrimSpace(config.Address)
	host := strings.TrimSpace(config.Host)
	username := strings.TrimSpace(config.Username)
	from, err := mail.ParseAddress(strings.TrimSpace(config.From))
	if err != nil || address == "" || host == "" || from.Address == "" {
		return nil, errAdminOTPMailerConfigInvalid
	}
	if parsedHost, _, err := net.SplitHostPort(address); err != nil ||
		!strings.EqualFold(parsedHost, host) {
		return nil, errAdminOTPMailerConfigInvalid
	}
	if strings.ContainsAny(host, "\r\n") ||
		strings.ContainsAny(from.String(), "\r\n") {
		return nil, errAdminOTPMailerConfigInvalid
	}
	if (username == "") != (config.Password == "") {
		return nil, errAdminOTPMailerConfigInvalid
	}

	timeout := config.Timeout
	if timeout <= 0 {
		timeout = adminOTPMailTimeout
	}

	return &smtpAdminOTPMailer{
		address:  address,
		host:     host,
		username: username,
		password: config.Password,
		from:     from,
		timeout:  timeout,
	}, nil
}

// SendOTP validates the complete message before opening a socket and requires
// TLS before credentials, addresses, or the short-lived code cross the network.
func (mailer *smtpAdminOTPMailer) SendOTP(
	ctx context.Context,
	recipient string,
	code string,
	expiresAt time.Time,
) error {
	if mailer == nil || ctx == nil || expiresAt.IsZero() ||
		!validAdminOTPCode(code) {
		return errAdminOTPMailInvalid
	}
	to, err := mail.ParseAddress(strings.TrimSpace(recipient))
	if err != nil || to.Address == "" || strings.ContainsAny(to.String(), "\r\n") {
		return errAdminOTPMailInvalid
	}

	deadline := time.Now().Add(mailer.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	dialer := net.Dialer{Deadline: deadline}
	connection, err := dialer.DialContext(ctx, "tcp", mailer.address)
	if err != nil {
		return newAdminOTPMailDeliveryError(adminOTPMailDeliveryStageConnect)
	}
	defer connection.Close()
	if err := connection.SetDeadline(deadline); err != nil {
		return newAdminOTPMailDeliveryError(adminOTPMailDeliveryStageConnect)
	}

	client, err := smtp.NewClient(connection, mailer.host)
	if err != nil {
		return newAdminOTPMailDeliveryError(adminOTPMailDeliveryStageConnect)
	}
	defer client.Close()
	if supported, _ := client.Extension("STARTTLS"); !supported {
		return newAdminOTPMailDeliveryError(adminOTPMailDeliveryStageTLS)
	}
	if err := client.StartTLS(&tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: mailer.host,
	}); err != nil {
		return newAdminOTPMailDeliveryError(adminOTPMailDeliveryStageTLS)
	}
	if mailer.username != "" {
		auth := smtp.PlainAuth(
			"",
			mailer.username,
			mailer.password,
			mailer.host,
		)
		if err := client.Auth(auth); err != nil {
			return newAdminOTPMailDeliveryError(
				adminOTPMailDeliveryStageAuthenticate,
			)
		}
	}
	if err := client.Mail(mailer.from.Address); err != nil {
		return newAdminOTPMailDeliveryError(adminOTPMailDeliveryStageSender)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return newAdminOTPMailDeliveryError(adminOTPMailDeliveryStageRecipient)
	}

	messageWriter, err := client.Data()
	if err != nil {
		return newAdminOTPMailDeliveryError(adminOTPMailDeliveryStageMessage)
	}
	message := adminOTPMailMessage(mailer.from, to, code, expiresAt)
	if _, err := io.WriteString(messageWriter, message); err != nil {
		_ = messageWriter.Close()
		return newAdminOTPMailDeliveryError(adminOTPMailDeliveryStageMessage)
	}
	if err := messageWriter.Close(); err != nil {
		return newAdminOTPMailDeliveryError(adminOTPMailDeliveryStageMessage)
	}
	// DATA Close has already received the server's acceptance response. QUIT is
	// connection cleanup, so a later disconnect must not turn accepted mail into
	// a false delivery failure or encourage a duplicate retry.
	_ = client.Quit()

	return nil
}

// adminOTPMailMessage returns a CRLF-canonical multipart message without
// attacker-owned body markup. Text-only clients receive the complete security
// guidance, while HTML clients receive the same content in the studio palette.
// The expiry is UTC so clients in different zones see one unambiguous instant.
func adminOTPMailMessage(
	from *mail.Address,
	to *mail.Address,
	code string,
	expiresAt time.Time,
) string {
	expiresAtText := expiresAt.UTC().Format("2006-01-02 15:04:05")
	textBody := fmt.Sprintf(
		"ZAFARMAND STUDIO\r\n"+
			"Administrator sign-in\r\n\r\n"+
			"Your four-digit verification code is:\r\n\r\n"+
			"%s\r\n\r\n"+
			"This code expires in five minutes, at %s UTC.\r\n"+
			"Enter it only on the Zafarmand administration sign-in page. "+
			"Zafarmand will never ask you to share this code.\r\n\r\n"+
			"If you did not request this email, you can safely ignore it.\r\n",
		code,
		expiresAtText,
	)
	htmlBody := fmt.Sprintf(
		"<!doctype html>\r\n"+
			"<html lang=\"en\"><body style=\"margin:0;background:#15130f;color:#eee9df;font-family:Arial,Helvetica,sans-serif;\">\r\n"+
			"<table role=\"presentation\" width=\"100%%\" cellspacing=\"0\" cellpadding=\"0\" style=\"background:#15130f;padding:32px 16px;\"><tr><td align=\"center\">\r\n"+
			"<table role=\"presentation\" width=\"100%%\" cellspacing=\"0\" cellpadding=\"0\" style=\"max-width:520px;border:1px solid #5f503c;border-radius:20px;background:#0d0c0a;\">\r\n"+
			"<tr><td style=\"padding:32px 32px 12px;color:#c9aa7b;font-size:12px;font-weight:700;letter-spacing:3px;\">ZAFARMAND STUDIO</td></tr>\r\n"+
			"<tr><td style=\"padding:0 32px;color:#eee9df;font-family:Georgia,'Times New Roman',serif;font-size:30px;line-height:1.2;\">Administrator sign-in</td></tr>\r\n"+
			"<tr><td style=\"padding:14px 32px 0;color:#aaa39a;font-size:15px;line-height:1.6;\">Use this four-digit verification code to continue:</td></tr>\r\n"+
			"<tr><td align=\"center\" style=\"padding:28px 32px;\"><div style=\"border:1px solid #c9aa7b;border-radius:14px;background:#181510;color:#f4e6cf;font-family:Consolas,'Courier New',monospace;font-size:42px;font-weight:700;letter-spacing:12px;line-height:1;padding:22px 10px 22px 22px;\">%s</div></td></tr>\r\n"+
			"<tr><td style=\"padding:0 32px 12px;color:#aaa39a;font-size:14px;line-height:1.6;\">This code expires in five minutes, at <strong style=\"color:#eee9df;\">%s UTC</strong>.</td></tr>\r\n"+
			"<tr><td style=\"padding:0 32px 32px;color:#777169;font-size:12px;line-height:1.6;\">Enter it only on the Zafarmand administration sign-in page. Zafarmand will never ask you to share this code. If you did not request this email, you can safely ignore it.</td></tr>\r\n"+
			"</table></td></tr></table>\r\n"+
			"</body></html>\r\n",
		code,
		expiresAtText,
	)

	return fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: Your Zafarmand administrator verification code\r\n"+
			"Date: %s\r\nMIME-Version: 1.0\r\n"+
			"Auto-Submitted: auto-generated\r\n"+
			"X-Auto-Response-Suppress: All\r\n"+
			"Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n"+
			"--%s\r\n"+
			"Content-Type: text/plain; charset=UTF-8\r\n"+
			"Content-Transfer-Encoding: 8bit\r\n\r\n%s\r\n"+
			"--%s\r\n"+
			"Content-Type: text/html; charset=UTF-8\r\n"+
			"Content-Transfer-Encoding: 8bit\r\n\r\n%s\r\n"+
			"--%s--\r\n",
		from.String(),
		to.String(),
		time.Now().UTC().Format(time.RFC1123Z),
		adminOTPMultipartBoundary,
		adminOTPMultipartBoundary,
		textBody,
		adminOTPMultipartBoundary,
		htmlBody,
		adminOTPMultipartBoundary,
	)
}

// validAdminOTPCode accepts exactly four ASCII digits; Unicode numerals are not
// canonical browser or email representations for this security boundary.
func validAdminOTPCode(code string) bool {
	if len(code) != adminOTPCodeLength {
		return false
	}
	for index := range code {
		if code[index] < '0' || code[index] > '9' {
			return false
		}
	}

	return true
}
