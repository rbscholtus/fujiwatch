package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	htmltemplate "html/template"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/diff"
	"github.com/rbscholtus/fujiwatch/internal/price"
	"github.com/rbscholtus/fujiwatch/internal/runner"
)

// Email sends the daily digest over SMTP with STARTTLS (README §8), e.g.
// Gmail on smtp.gmail.com:587 with an app password.
type Email struct {
	Addr string // host:port
	User string
	Pass string
	From string
	To   []string

	// send is smtp.SendMail; tests may replace it.
	send func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// Name returns "email".
func (*Email) Name() string { return "email" }

// Send renders msg and delivers it. net/smtp upgrades to TLS via STARTTLS
// when offered, and PlainAuth refuses to send the password over an
// unencrypted connection to anything but localhost.
func (e *Email) Send(ctx context.Context, msg *Message) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("email: %w", err)
	}
	raw, err := e.Render(msg)
	if err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(e.Addr)
	if err != nil {
		return fmt.Errorf("email: bad SMTP address %q: %w", e.Addr, err)
	}
	send := e.send
	if send == nil {
		send = smtp.SendMail
	}
	if err := send(e.Addr, smtp.PlainAuth("", e.User, e.Pass, host), e.From, e.To, raw); err != nil {
		return fmt.Errorf("email: %w", err)
	}
	return nil
}

// Render builds the RFC 5322 message: multipart/alternative with a plain
// text and an HTML part, both quoted-printable UTF-8.
func (e *Email) Render(msg *Message) ([]byte, error) {
	view := newView(msg)

	var text, html bytes.Buffer
	if err := textTmpl.Execute(&text, view); err != nil {
		return nil, fmt.Errorf("email: render text: %w", err)
	}
	if err := htmlTmpl.Execute(&html, view); err != nil {
		return nil, fmt.Errorf("email: render html: %w", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, part := range []struct {
		ctype string
		data  []byte
	}{
		{"text/plain; charset=utf-8", text.Bytes()},
		{"text/html; charset=utf-8", html.Bytes()},
	} {
		w, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.ctype},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, fmt.Errorf("email: %w", err)
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := qp.Write(part.data); err != nil {
			return nil, fmt.Errorf("email: %w", err)
		}
		if err := qp.Close(); err != nil {
			return nil, fmt.Errorf("email: %w", err)
		}
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("email: %w", err)
	}

	var out bytes.Buffer
	hdr := func(k, v string) { fmt.Fprintf(&out, "%s: %s\r\n", k, v) }
	hdr("From", e.From)
	hdr("To", strings.Join(e.To, ", "))
	hdr("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	hdr("Date", msg.Time.Format(time.RFC1123Z))
	hdr("Message-ID", messageID(e.From))
	hdr("MIME-Version", "1.0")
	hdr("Content-Type", `multipart/alternative; boundary="`+mw.Boundary()+`"`)
	out.WriteString("\r\n")
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

func messageID(from string) string {
	domain := "fujiwatch.local"
	if _, d, ok := strings.Cut(from, "@"); ok {
		domain = strings.Trim(d, "> ")
	}
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "<" + hex.EncodeToString(b) + "@" + domain + ">"
}

// ── view model shared by both templates ──────────────────────────

type eventView struct {
	Label, Shop, Product, Condition, Price, Title, URL, Detail string
	Important                                                  bool
}

type bestView struct {
	Name, Condition, Price, Shop, URL string
	Count                             int
}

type unmatchedGroup struct {
	Status string
	Items  []runner.Unmatched
}

type view struct {
	Subject   string
	Time      string
	Events    []eventView
	Best      []bestView
	Sources   []runner.SourceReport
	Broken    int
	Unmatched []unmatchedGroup
}

func newView(msg *Message) view {
	v := view{Subject: msg.Subject, Time: msg.Time.In(saigon).Format("2006-01-02 15:04 (UTC+7)"), Sources: msg.Sources}

	for _, e := range msg.Events {
		name := msg.Names[e.Product]
		if name == "" {
			name = e.Product
		}
		v.Events = append(v.Events, eventView{
			Label:     runner.EventLabel(e.Type),
			Shop:      e.Shop,
			Product:   name,
			Condition: e.Condition,
			Price:     runner.EventPrice(e),
			Title:     e.Title,
			URL:       e.URL,
			Detail:    e.Detail,
			Important: e.Type != diff.PriceRise && e.Type != diff.OutOfStock && e.Type != diff.Gone,
		})
	}

	for _, b := range msg.Best {
		bv := bestView{Name: b.Name, Condition: b.Condition, Count: b.Count, Price: "–"}
		if b.Offer != nil {
			bv.Price = price.FormatVND(b.Offer.PriceVND)
			bv.Shop = b.Offer.Shop
			bv.URL = b.Offer.URL
		}
		v.Best = append(v.Best, bv)
	}

	for _, s := range msg.Sources {
		if s.Err != "" {
			v.Broken++
		}
	}

	// Ambiguous and mismatched first: those need attention. Plain
	// unmatched offers (other models on a category page) come last.
	for _, status := range []string{"mismatch", "ambiguous", "unmatched"} {
		g := unmatchedGroup{Status: status}
		for _, u := range msg.Unmatched {
			if u.Status == status {
				g.Items = append(g.Items, u)
			}
		}
		if len(g.Items) > 0 {
			v.Unmatched = append(v.Unmatched, g)
		}
	}
	return v
}

var textTmpl = texttemplate.Must(texttemplate.New("text").Parse(`{{.Subject}}
{{.Time}}

EVENTS ({{len .Events}})
{{range .Events}}{{if .Detail}}- {{.Label}}: {{.Shop}}: {{.Detail}}
  {{.URL}}
{{else}}- {{.Label}}: {{.Product}} ({{.Condition}}) at {{.Shop}}: {{.Price}}
  {{.Title}}
  {{.URL}}
{{end}}{{else}}No changes.
{{end}}
BEST PRICES
{{range .Best}}- {{.Name}} ({{.Condition}}): {{.Price}}{{if .Shop}} at {{.Shop}} {{.URL}}{{end}} [{{.Count}} known]
{{end}}
SOURCES ({{len .Sources}}, {{.Broken}} failing)
{{range .Sources}}- {{if .Err}}FAIL{{else}}ok{{end}} {{.Shop}} {{.Mode}}: {{.Offers}} offers, {{.Matched}} matched  {{.URL}}{{if .Err}}
  {{.Err}}{{end}}
{{end}}{{range .Unmatched}}
{{.Status}} ({{len .Items}})
{{range .Items}}- {{.Shop}}: {{.Title}}{{if .Candidates}} {{.Candidates}}{{end}}
  {{.URL}}
{{end}}{{end}}`))

var htmlTmpl = htmltemplate.Must(htmltemplate.New("html").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>{{.Subject}}</title></head>
<body style="font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;font-size:14px;color:#1f2328;max-width:760px;margin:0 auto;padding:16px">
<h2 style="margin:0 0 4px">{{.Subject}}</h2>
<div style="color:#656d76;margin-bottom:16px">{{.Time}}</div>

<h3>Events ({{len .Events}})</h3>
{{if .Events}}<table cellpadding="6" style="border-collapse:collapse;width:100%">
{{range .Events}}<tr style="border-top:1px solid #d0d7de;vertical-align:top">
<td style="white-space:nowrap;font-weight:{{if .Important}}bold{{else}}normal{{end}}">{{.Label}}</td>
{{if .Detail}}<td colspan="2">{{.Shop}}: {{.Detail}}<br><a href="{{.URL}}" style="color:#656d76;font-size:12px">{{.URL}}</a></td>
{{else}}<td><a href="{{.URL}}">{{.Product}}</a> <span style="color:#656d76">({{.Condition}}, {{.Shop}})</span><br><span style="color:#656d76;font-size:12px">{{.Title}}</span></td>
<td style="white-space:nowrap;text-align:right">{{.Price}}</td>{{end}}
</tr>
{{end}}</table>{{else}}<p>No changes.</p>{{end}}

<h3>Best prices</h3>
<table cellpadding="6" style="border-collapse:collapse;width:100%">
<tr style="text-align:left;color:#656d76"><th>Product</th><th>Cond.</th><th style="text-align:right">Price</th><th>Shop</th><th style="text-align:right">Known</th></tr>
{{range .Best}}<tr style="border-top:1px solid #d0d7de">
<td>{{.Name}}</td><td>{{.Condition}}</td>
<td style="text-align:right;white-space:nowrap">{{if .URL}}<a href="{{.URL}}">{{.Price}}</a>{{else}}{{.Price}}{{end}}</td>
<td>{{.Shop}}</td><td style="text-align:right">{{.Count}}</td>
</tr>
{{end}}</table>

<h3>Sources ({{len .Sources}}, {{.Broken}} failing)</h3>
<table cellpadding="4" style="border-collapse:collapse;width:100%;font-size:13px">
{{range .Sources}}<tr style="border-top:1px solid #d0d7de;vertical-align:top">
<td style="color:{{if .Err}}#cf222e{{else}}#1a7f37{{end}};font-weight:bold">{{if .Err}}FAIL{{else}}ok{{end}}</td>
<td>{{.Shop}}</td><td>{{.Mode}}</td><td style="white-space:nowrap">{{.Offers}} / {{.Matched}}</td>
<td><a href="{{.URL}}" style="color:#656d76">{{.URL}}</a>{{if .Err}}<br><span style="color:#cf222e">{{.Err}}</span>{{end}}</td>
</tr>
{{end}}</table>

{{range .Unmatched}}<h3 style="text-transform:capitalize">{{.Status}} ({{len .Items}})</h3>
<ul style="font-size:12px;color:#656d76;padding-left:18px">
{{range .Items}}<li>{{.Shop}}: <a href="{{.URL}}" style="color:#656d76">{{.Title}}</a>{{if .Candidates}} {{.Candidates}}{{end}}</li>
{{end}}</ul>
{{end}}
<p style="color:#8c959f;font-size:12px;margin-top:24px">Sent by fujiwatch.</p>
</body></html>
`))
