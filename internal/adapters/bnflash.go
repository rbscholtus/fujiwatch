package adapters

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/rbscholtus/fujiwatch/internal/match"
)

// bnFlashAjaxPath is WordPress's AJAX endpoint (allowed by robots.txt),
// where the bn-flash-sale plugin answers action=bnfs_get_products.
const bnFlashAjaxPath = "/wp-admin/admin-ajax.php"

// BNFlash reads bncamera's flash sale ("BNCamera Flash Sale", plugin
// bn-flash-sale). The homepage holds the schedule: a campaign, day tabs
// (day_auto_YYYYMMDD) and time slots (10–12h, 12–14h, 20–22h, each with
// data-start / data-end), for today and the next two days. Each slot's
// products are loaded with a POST to admin-ajax.php; NextPage requests
// every slot that has not ended, one per page, keeping the rest in State.
type BNFlash struct{}

// NewBNFlash returns the bncamera flash-sale adapter.
func NewBNFlash() BNFlash { return BNFlash{} }

// Name returns "bncamera".
func (BNFlash) Name() string { return "bncamera" }

// bnSlot is one flash-sale slot: a day tab and a time slot.
type bnSlot struct {
	Day   string `json:"day"`
	Slot  string `json:"slot"`
	Start int64  `json:"start"` // Unix seconds
	End   int64  `json:"end"`
}

// bnFlashResponse is the answer to action=bnfs_get_products.
type bnFlashResponse struct {
	Success bool `json:"success"`
	Data    struct {
		HTML      string `json:"html"`
		Message   string `json:"message"`
		Countdown struct {
			Start int64 `json:"start"`
			End   int64 `json:"end"`
		} `json:"countdown"`
	} `json:"data"`
}

// Extract reads a slot's products from a bnfs_get_products response. The
// homepage itself yields no offers: its slots are requested as pages.
func (b BNFlash) Extract(pageURL string, _ *goquery.Document, raw []byte) ([]Offer, error) {
	if !looksLikeJSON(raw) {
		return nil, nil
	}
	var r bnFlashResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("decode flash sale json: %w", err)
	}
	if !r.Success {
		return nil, fmt.Errorf("flash sale request refused: %q", r.Data.Message)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(r.Data.HTML))
	if err != nil {
		return nil, fmt.Errorf("parse flash sale html: %w", err)
	}
	start, end := time.Unix(r.Data.Countdown.Start, 0).UTC(), time.Unix(r.Data.Countdown.End, 0).UTC()

	var offers []Offer
	doc.Find("article.bnfs-product-card").Each(func(_ int, card *goquery.Selection) {
		link := card.Find(".bnfs-product-card__title a").First()
		title := strings.Join(strings.Fields(link.Text()), " ")
		href, _ := link.Attr("href")
		if title == "" || href == "" {
			return
		}
		o := Offer{
			Shop:       b.Name(),
			URL:        resolveURL(pageURL, html.UnescapeString(strings.TrimSpace(href))),
			Title:      title,
			PriceVND:   parsePrice(card.Find(".bnfs-product-card__sale-price").First().Text()),
			RegularVND: parsePrice(card.Find(".bnfs-product-card__prices del").First().Text()),
			FlashStart: start,
			FlashEnd:   end,
		}
		// The variant is only in the link ("…?attribute_pa_phien-ban=
		// lens-16-50mm-…"); name it, so kits don't match a body.
		if v := variantLabel(o.URL); v != "" {
			o.Title += " (" + v + ")"
		}
		if m := soldRe.FindStringSubmatch(card.Find(".bnfs-product-card__sold").Text()); m != nil {
			sold, _ := strconv.Atoi(m[1])
			total, _ := strconv.Atoi(m[2])
			o.Quota = m[1] + "/" + m[2]
			o.InStock = new(sold < total)
		}
		if match.DetectCondition(o.Title, "") == match.ConditionUsed {
			o.Condition = match.ConditionUsed
		}
		offers = append(offers, o)
	})
	return offers, nil
}

// soldRe reads "Đã bán 0/7 suất" (sold 0 of 7 units on offer).
var soldRe = regexp.MustCompile(`(\d+)\s*/\s*(\d+)`)

// variantLabel names a WooCommerce variation from its link's attributes:
// "attribute_pa_mau=black&attribute_pa_phien-ban=body-only" → "black, body only".
func variantLabel(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	q := u.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		if strings.HasPrefix(k, "attribute_") {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	vals := make([]string, 0, len(keys))
	for _, k := range keys {
		if v := strings.TrimSpace(strings.ReplaceAll(q.Get(k), "-", " ")); v != "" {
			vals = append(vals, v)
		}
	}
	return strings.Join(vals, ", ")
}

// NextPage requests the slots: from the homepage, the first slot that has
// not ended, with the others in State; from a slot's response, the next.
func (b BNFlash) NextPage(cur Page, raw []byte) (Page, error) {
	var base url.Values
	var slots []bnSlot
	if cur.Form == nil {
		var err error
		if base, slots, err = bnSchedule(raw); err != nil {
			return Page{}, err
		}
		if len(slots) == 0 {
			return Page{}, nil // no flash sale
		}
		u, err := url.Parse(cur.URL)
		if err != nil {
			return Page{}, fmt.Errorf("parse url: %w", err)
		}
		cur.URL = (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: bnFlashAjaxPath}).String()
	} else {
		base = url.Values{}
		for _, k := range []string{"action", "nonce", "campaign_id", "tab_id", "limit", "ui"} {
			base.Set(k, cur.Form.Get(k))
		}
		if err := json.Unmarshal([]byte(cur.State), &slots); err != nil {
			return Page{}, fmt.Errorf("flash sale state: %w", err)
		}
	}
	if len(slots) == 0 {
		return Page{}, nil
	}
	next, rest := slots[0], slots[1:]
	state, err := json.Marshal(rest)
	if err != nil {
		return Page{}, fmt.Errorf("flash sale state: %w", err)
	}
	form := url.Values{}
	for k, v := range base {
		form[k] = slices.Clone(v)
	}
	form.Set("day_id", next.Day)
	form.Set("timeslot_id", next.Slot)
	return Page{URL: cur.URL, Form: form, State: string(state)}, nil
}

// saigonDay names a day tab the way the plugin does: day_auto_YYYYMMDD in
// Vietnam time.
func saigonDay(unix int64) string {
	return "day_auto_" + time.Unix(unix, 0).In(saigon).Format("20060102")
}

// bnSchedule reads the homepage's flash-sale schedule: the request fields
// shared by every slot, and the slots that have not ended by the page's
// server time, in order.
func bnSchedule(raw []byte) (url.Values, []bnSlot, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, fmt.Errorf("parse html: %w", err)
	}
	section := doc.Find("section.bnfs-section").First()
	if section.Length() == 0 {
		return nil, nil, nil // no flash sale running or scheduled
	}
	m := bnfsPublicRe.FindSubmatch(raw)
	if m == nil {
		return nil, nil, errors.New("flash sale: bnfsPublic (nonce) not found")
	}
	var pub struct {
		Nonce      string `json:"nonce"`
		ServerTime string `json:"serverTime"`
	}
	if err := json.Unmarshal(m[1], &pub); err != nil {
		return nil, nil, fmt.Errorf("flash sale: bnfsPublic: %w", err)
	}
	if pub.Nonce == "" {
		return nil, nil, errors.New("flash sale: bnfsPublic has no nonce")
	}
	now, _ := strconv.ParseInt(pub.ServerTime, 10, 64)

	tab, _ := section.Find(`[data-category="all"][data-tab-id]`).First().Attr("data-tab-id")
	if tab == "" {
		tab, _ = section.Attr("data-bnfs-active-tab")
	}
	campaign, _ := section.Attr("data-bnfs-campaign")
	days := map[string]bool{}
	section.Find("[data-day-id]").Each(func(_ int, s *goquery.Selection) {
		days[s.AttrOr("data-day-id", "")] = true
	})

	var slots []bnSlot
	seen := map[string]bool{}
	section.Find("[data-timeslot-id][data-start][data-end]").Each(func(_ int, s *goquery.Selection) {
		start, _ := strconv.ParseInt(s.AttrOr("data-start", ""), 10, 64)
		end, _ := strconv.ParseInt(s.AttrOr("data-end", ""), 10, 64)
		sl := bnSlot{Day: saigonDay(start), Slot: s.AttrOr("data-timeslot-id", ""), Start: start, End: end}
		key := sl.Day + "|" + sl.Slot
		if sl.Slot == "" || start == 0 || end <= now || !days[sl.Day] || seen[key] {
			return
		}
		seen[key] = true
		slots = append(slots, sl)
	})
	base := url.Values{
		"action": {"bnfs_get_products"}, "nonce": {pub.Nonce}, "campaign_id": {campaign},
		"tab_id": {tab}, "limit": {"0"}, "ui": {""},
	}
	return base, slots, nil
}

var bnfsPublicRe = regexp.MustCompile(`var bnfsPublic = (\{.*?\});`)
