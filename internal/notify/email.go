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

// Email sends the daily digest over SMTP with STARTTLS (docs/usage.md, Notifications), e.g.
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

type weekView struct {
	Name, Condition, This, Last, Change, URL, Shop string
	Down                                           bool
}

type unmatchedGroup struct {
	Status string // mismatch | ambiguous | near miss
	Items  []unmatchedView
}

type unmatchedView struct {
	Shop, Title, URL string
	Products         []string // candidates, or the near-missed products
}

type view struct {
	Subject   string
	Time      string
	Events    []eventView
	Best      []bestView
	Sources   []runner.SourceReport
	Broken    int
	Unmatched []unmatchedGroup
	Other     int    // unmatched offers not listed (not close to any product)
	OtherShop string // Other per shop, e.g. "vjshop 250, kyma 30"
	Baseline  int    // events from a source's first run
	Weekly    []weekView
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
		if e.Baseline {
			v.Baseline++
		}
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

	for _, w := range msg.Weekly {
		wv := weekView{Name: w.Name, Condition: w.Condition, This: "–", Last: "–", URL: w.This.URL, Shop: w.This.Shop}
		if w.This.PriceVND > 0 {
			wv.This = price.FormatVND(w.This.PriceVND)
		}
		if w.Last.PriceVND > 0 {
			wv.Last = price.FormatVND(w.Last.PriceVND)
		}
		if w.This.PriceVND > 0 && w.Last.PriceVND > 0 {
			pct := float64(w.This.PriceVND-w.Last.PriceVND) * 100 / float64(w.Last.PriceVND)
			wv.Change = fmt.Sprintf("%+.1f%%", pct)
			wv.Down = pct < 0
		}
		v.Weekly = append(v.Weekly, wv)
	}

	v.Unmatched, v.Other, v.OtherShop = groupUnmatched(msg.Unmatched)
	return v
}

// groupUnmatched lists the offers that need attention: product pages whose
// title doesn't match their product, titles matching several products, and
// near misses (a watched product named, its mount or version not
// confirmed). Every other unmatched offer (other models on a category page)
// is only counted, per shop.
func groupUnmatched(all []runner.Unmatched) (groups []unmatchedGroup, other int, otherShop string) {
	byStatus := map[string]*unmatchedGroup{}
	order := []string{"mismatch", "ambiguous", "near miss"}
	perShop := map[string]int{}
	var shops []string
	for _, u := range all {
		status, products := u.Status, u.Candidates
		if status == "unmatched" {
			if len(u.NearMiss) == 0 {
				if perShop[u.Shop] == 0 {
					shops = append(shops, u.Shop)
				}
				perShop[u.Shop]++
				other++
				continue
			}
			status, products = "near miss", u.NearMiss
		}
		g := byStatus[status]
		if g == nil {
			g = &unmatchedGroup{Status: status}
			byStatus[status] = g
		}
		g.Items = append(g.Items, unmatchedView{Shop: u.Shop, Title: u.Title, URL: u.URL, Products: products})
	}
	for _, s := range order {
		if g := byStatus[s]; g != nil {
			groups = append(groups, *g)
		}
	}
	parts := make([]string, 0, len(shops))
	for _, s := range shops {
		parts = append(parts, fmt.Sprintf("%s %d", s, perShop[s]))
	}
	return groups, other, strings.Join(parts, ", ")
}

var textTmpl = texttemplate.Must(texttemplate.New("text").Parse(`{{.Subject}}
{{.Time}}

EVENTS ({{len .Events}})
{{if .Baseline}}{{.Baseline}} from a source's first run: recorded as a baseline, not sent to WhatsApp.
{{end}}{{range .Events}}{{if .Detail}}- {{.Label}}: {{.Shop}}: {{.Detail}}
  {{.URL}}
{{else}}- {{.Label}}: {{.Product}} ({{.Condition}}) at {{.Shop}}: {{.Price}}
  {{.Title}}
  {{.URL}}
{{end}}{{else}}No changes.
{{end}}
{{if .Weekly}}THIS WEEK (lowest price, vs last week)
{{range .Weekly}}- {{.Name}} ({{.Condition}}): {{.This}}{{if .Shop}} at {{.Shop}}{{end}} | last week {{.Last}}{{if .Change}} ({{.Change}}){{end}}
{{end}}
{{end}}BEST PRICES
{{range .Best}}- {{.Name}} ({{.Condition}}): {{.Price}}{{if .Shop}} at {{.Shop}} {{.URL}}{{end}} [{{.Count}} known]
{{end}}
SOURCES ({{len .Sources}}, {{.Broken}} failing)
{{range .Sources}}- {{if .Err}}FAIL{{else}}ok{{end}} {{.Shop}} {{.Mode}}: {{.Offers}} offers, {{.Matched}} matched  {{.URL}}{{if .Err}}
  {{.Err}}{{end}}
{{end}}{{range .Unmatched}}
{{.Status}} ({{len .Items}})
{{range .Items}}- {{.Shop}}: {{.Title}}{{if .Products}} {{.Products}}{{end}}
  {{.URL}}
{{end}}{{end}}{{if .Other}}
Other unmatched offers: {{.Other}} ({{.OtherShop}})
{{end}}`))

var htmlTmpl = htmltemplate.Must(htmltemplate.New("html").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>{{.Subject}}</title></head>
<body style="font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;font-size:14px;color:#1f2328;max-width:760px;margin:0 auto;padding:16px">
<h2 style="margin:0 0 4px">{{.Subject}}</h2>
<div style="color:#656d76;margin-bottom:16px">{{.Time}}</div>

<h3>Events ({{len .Events}})</h3>
{{if .Baseline}}<p style="color:#656d76">{{.Baseline}} from a source's first run: recorded as a baseline, not sent to WhatsApp.</p>
{{end}}{{if .Events}}<table cellpadding="6" style="border-collapse:collapse;width:100%">
{{range .Events}}<tr style="border-top:1px solid #d0d7de;vertical-align:top">
<td style="white-space:nowrap;font-weight:{{if .Important}}bold{{else}}normal{{end}}">{{.Label}}</td>
{{if .Detail}}<td colspan="2">{{.Shop}}: {{.Detail}}<br><a href="{{.URL}}" style="color:#656d76;font-size:12px">{{.URL}}</a></td>
{{else}}<td><a href="{{.URL}}">{{.Product}}</a> <span style="color:#656d76">({{.Condition}}, {{.Shop}})</span><br><span style="color:#656d76;font-size:12px">{{.Title}}</span></td>
<td style="white-space:nowrap;text-align:right">{{.Price}}</td>{{end}}
</tr>
{{end}}</table>{{else}}<p>No changes.</p>{{end}}

{{if .Weekly}}<h3>This week</h3>
<table cellpadding="6" style="border-collapse:collapse;width:100%">
<tr style="text-align:left;color:#656d76"><th>Product</th><th>Cond.</th><th style="text-align:right">This week</th><th style="text-align:right">Last week</th><th style="text-align:right">Change</th></tr>
{{range .Weekly}}<tr style="border-top:1px solid #d0d7de">
<td>{{.Name}}</td><td>{{.Condition}}</td>
<td style="text-align:right;white-space:nowrap">{{if .URL}}<a href="{{.URL}}">{{.This}}</a>{{else}}{{.This}}{{end}}</td>
<td style="text-align:right;white-space:nowrap">{{.Last}}</td>
<td style="text-align:right;color:{{if .Down}}#1a7f37{{else}}#656d76{{end}}">{{.Change}}</td>
</tr>
{{end}}</table>
{{end}}
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
{{range .Items}}<li>{{.Shop}}: <a href="{{.URL}}" style="color:#656d76">{{.Title}}</a>{{if .Products}} {{.Products}}{{end}}</li>
{{end}}</ul>
{{end}}{{if .Other}}<p style="font-size:12px;color:#656d76">Other unmatched offers: {{.Other}} ({{.OtherShop}})</p>
{{end}}
<p style="color:#8c959f;font-size:12px;margin-top:24px">Sent by fujiwatch.</p>
</body></html>
`))
