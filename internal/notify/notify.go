// Package notify sends run results: an email digest (SMTP) and short
// WhatsApp alerts (CallMeBot).
package notify

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/diff"
	"github.com/rbscholtus/fujiwatch/internal/runner"
	"github.com/rbscholtus/fujiwatch/internal/state"
)

// Notifier delivers a run's results somewhere.
type Notifier interface {
	Name() string
	Send(ctx context.Context, msg *Message) error
}

// Message is everything a notifier may want to show.
type Message struct {
	Subject   string
	Time      time.Time
	Events    []diff.Event
	Best      []Best
	Sources   []runner.SourceReport
	Unmatched []runner.Unmatched
	Names     map[string]string // product ID → display name
}

// Best is the cheapest known offer of one product in one condition.
type Best struct {
	Product   string
	Name      string
	Condition string // new | used
	Offer     *state.Offer
	Count     int // offers known for this product+condition
}

// BuildMessage assembles the message for a finished run. st must already
// contain this run's changes.
func BuildMessage(cfg *config.Config, rep *runner.Report, st *state.State) *Message {
	names := map[string]string{}
	for _, p := range cfg.Watchlist.Products {
		names[p.ID] = p.Name
	}
	return &Message{
		Subject:   Subject(rep.Events),
		Time:      rep.Time,
		Events:    rep.Events,
		Best:      BestPrices(cfg.Watchlist.Products, st),
		Sources:   rep.Sources,
		Unmatched: rep.Unmatched,
		Names:     names,
	}
}

// subjectOrder lists event types in subject-line order.
var subjectOrder = []struct {
	t        diff.Type
	one, few string
}{
	{diff.PriceDrop, "price drop", "price drops"},
	{diff.BackInStock, "back in stock", "back in stock"},
	{diff.NewListing, "new listing", "new listings"},
	{diff.AdapterBroken, "broken source", "broken sources"},
	{diff.Gone, "gone", "gone"},
	{diff.OutOfStock, "out of stock", "out of stock"},
	{diff.PriceRise, "price rise", "price rises"},
}

// Subject summarises events, e.g. "fujiwatch: 2 price drops, 1 new listing".
func Subject(events []diff.Event) string {
	counts := map[diff.Type]int{}
	for _, e := range events {
		counts[e.Type]++
	}
	var parts []string
	for _, s := range subjectOrder {
		switch n := counts[s.t]; n {
		case 0:
		case 1:
			parts = append(parts, "1 "+s.one)
		default:
			parts = append(parts, fmt.Sprintf("%d %s", n, s.few))
		}
	}
	if len(parts) == 0 {
		return "fujiwatch: no changes"
	}
	return "fujiwatch: " + strings.Join(parts, ", ")
}

// BestPrices returns, per watched product (in watchlist order), the cheapest
// offer with a known price that is not known to be out of stock, separately
// for new and used.
func BestPrices(products []config.Product, st *state.State) []Best {
	out := make([]Best, 0, 2*len(products))
	for _, p := range products {
		for _, cond := range []string{"new", "used"} {
			b := Best{Product: p.ID, Name: p.Name, Condition: cond}
			for _, o := range st.Offers {
				if o.Product != p.ID || o.Condition != cond {
					continue
				}
				b.Count++
				if o.PriceVND <= 0 || (o.InStock != nil && !*o.InStock) {
					continue
				}
				if b.Offer == nil || o.PriceVND < b.Offer.PriceVND ||
					(o.PriceVND == b.Offer.PriceVND && o.URL < b.Offer.URL) {
					b.Offer = o
				}
			}
			out = append(out, b)
		}
	}
	return out
}

// ShouldEmail applies the email policy from sources.yaml: "always", or
// "changes" (only with events) plus a heartbeat on one weekday.
func ShouldEmail(n config.Notify, events []diff.Event, now time.Time) bool {
	if n.Email != config.EmailChanges {
		return true
	}
	if len(events) > 0 {
		return true
	}
	return n.Heartbeat != "" && strings.EqualFold(now.In(saigon).Weekday().String(), n.Heartbeat)
}

// WhatsAppTypes are the events worth an instant message (README §8).
var WhatsAppTypes = []diff.Type{diff.PriceDrop, diff.BackInStock, diff.NewListing}

func instant(events []diff.Event) []diff.Event {
	var out []diff.Event
	for _, e := range events {
		if slices.Contains(WhatsAppTypes, e.Type) {
			out = append(out, e)
		}
	}
	return out
}

// saigon is Vietnam time (no DST), for weekday checks and display.
var saigon = time.FixedZone("ICT", 7*60*60)
