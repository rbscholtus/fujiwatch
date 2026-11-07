package adapters

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// kymaMorePath is kyma's "Xem thêm N sản phẩm" (show N more) endpoint. It
// only accepts POST and returns {"list": "<li>…cards…</li>", "paging": n,
// "remainingProducts": n}.
const kymaMorePath = "/Product/ShowMoreNextProCate"

// kymaPageSize is asked of the endpoint; one request then returns a whole
// category (the largest has about 100 items).
const kymaPageSize = 300

// Kyma handles kyma.vn. Category pages show 30 items; the rest is loaded
// with a POST to kymaMorePath, which NextPage returns as the next page.
type Kyma struct {
	CSS

	more Layout // cards in the endpoint's "list" HTML
}

// NewKyma returns the kyma adapter.
func NewKyma() Kyma {
	return Kyma{
		CSS: NewCSS("kyma", Layout{
			Name:  "category",
			Card:  "li.li-product",
			Title: "a.item__name",
			Link:  "a.item__name",
			Price: ".item__price > span:first-child",
		}),
		more: Layout{
			Name:  "show more",
			Card:  ".item__product",
			Title: "a.item__name",
			Link:  "a.item__name",
			Price: ".item__price > span:first-child",
		},
	}
}

type kymaMore struct {
	List      string `json:"list"`
	Paging    int    `json:"paging"`
	Remaining int    `json:"remainingProducts"`
}

// Extract reads a category or product page, or a show-more response.
func (k Kyma) Extract(pageURL string, doc *goquery.Document, raw []byte) ([]Offer, error) {
	if !looksLikeJSON(raw) {
		return k.CSS.Extract(pageURL, doc, raw)
	}
	var m kymaMore
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("decode show-more json: %w", err)
	}
	list, err := goquery.NewDocumentFromReader(strings.NewReader("<ul>" + m.List + "</ul>"))
	if err != nil {
		return nil, fmt.Errorf("parse show-more list: %w", err)
	}
	return k.more.extract(k.shop, pageURL, list), nil
}

// NextPage turns a category page with remaining products into a POST for
// all of them, and follows a show-more response that still reports some.
func (Kyma) NextPage(cur Page, raw []byte) (Page, error) {
	if looksLikeJSON(raw) {
		var m kymaMore
		if err := json.Unmarshal(raw, &m); err != nil {
			return Page{}, fmt.Errorf("decode show-more json: %w", err)
		}
		if m.Remaining <= 0 || cur.Form == nil {
			return Page{}, nil
		}
		form := url.Values{}
		maps.Copy(form, cur.Form)
		form.Set("paging", strconv.Itoa(m.Paging))
		return Page{URL: cur.URL, Form: form}, nil
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		return Page{}, fmt.Errorf("parse html: %w", err)
	}
	text := func(id string) string { return strings.TrimSpace(doc.Find("#" + id).First().Text()) }
	remaining, _ := strconv.Atoi(text("remainingProducts"))
	if text("categoryId") == "" || remaining <= 0 {
		return Page{}, nil // a product page, or everything is shown
	}
	u, err := url.Parse(cur.URL)
	if err != nil {
		return Page{}, fmt.Errorf("parse url: %w", err)
	}
	more := url.URL{Scheme: u.Scheme, Host: u.Host, Path: kymaMorePath}
	return Page{URL: more.String(), Form: url.Values{
		"categoryId": {text("categoryId")},
		"paging":     {"0"},
		"pageSize":   {strconv.Itoa(kymaPageSize)},
		"sortBy":     {text("sortBy")},
		"priceMin":   {text("priceMinMore")},
		"priceMax":   {text("priceMaxMore")},
	}}, nil
}
