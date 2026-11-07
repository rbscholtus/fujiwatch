package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/config"
)

func readFlash(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testdataDir, "bncamera", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestForSourceFlash(t *testing.T) {
	if _, ok := ForSource("bncamera", config.ModeFlash).(BNFlash); !ok {
		t.Error("bncamera flash source does not get the flash adapter")
	}
	if _, ok := ForSource("bncamera", config.ModeListing).(WooCommerce); !ok {
		t.Error("bncamera listing source does not get the WooCommerce adapter")
	}
	if _, err := Extract(ForSource("kyma", config.ModeFlash), "https://kyma.vn/", []byte("<html></html>")); err == nil {
		t.Error("a flash source for a shop without a flash adapter must fail")
	}
}

// The homepage (2026-11-07 21:21 ICT) has 3 days × 3 slots; the two that
// ended are skipped, the other 7 are requested one by one, then the end.
func TestBNFlashSchedule(t *testing.T) {
	b := NewBNFlash()
	home := readFlash(t, "flash-home.html")
	if offers, err := Extract(b, "https://bncamera.com/", home); err != nil || len(offers) != 0 {
		t.Fatalf("homepage: %d offers, %v; want none (slots are pages)", len(offers), err)
	}

	page, err := b.NextPage(Page{URL: "https://bncamera.com/"}, home)
	if err != nil {
		t.Fatal(err)
	}
	if page.URL != "https://bncamera.com/wp-admin/admin-ajax.php" || page.Form.Get("action") != "bnfs_get_products" ||
		page.Form.Get("nonce") == "" || page.Form.Get("campaign_id") != "1" || page.Form.Get("tab_id") != "tab_w2pn03k5" {
		t.Fatalf("first slot request = %+v", page)
	}
	var got []string
	for !page.IsZero() {
		got = append(got, page.Form.Get("day_id")+" "+page.Form.Get("timeslot_id"))
		if len(got) > 20 {
			t.Fatal("no end")
		}
		page, err = b.NextPage(page, readFlash(t, "flash-slot.json"))
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"day_auto_20261107 slot_jf3mjqaa", // 20–22h, running
		"day_auto_20261108 slot_tntgfwpe", "day_auto_20261108 slot_kld5oo3d", "day_auto_20261108 slot_jf3mjqaa",
		"day_auto_20261109 slot_tntgfwpe", "day_auto_20261109 slot_kld5oo3d", "day_auto_20261109 slot_jf3mjqaa",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("slots requested:\n%v\nwant\n%v", got, want)
	}

	// A homepage without a flash sale: no slots, no error.
	if p, err := b.NextPage(Page{URL: "https://bncamera.com/"}, []byte("<html><body>no sale</body></html>")); err != nil || !p.IsZero() {
		t.Errorf("no flash sale: %+v, %v", p, err)
	}
}

func TestBNFlashSlot(t *testing.T) {
	offers, err := Extract(NewBNFlash(), "https://bncamera.com/", readFlash(t, "flash-slot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 5 {
		t.Fatalf("%d offers, want 5", len(offers))
	}
	o := offers[0]
	start := time.Date(2026, 11, 8, 13, 0, 0, 0, time.UTC) // 20:00 ICT
	if o.Title != "Sony ZV-E10 II (black, chinh hang, lens 16 50mm f 3 5 5 6 oss ii)" ||
		o.URL != "https://bncamera.com/san-pham/sony-zv-e10-ii?attribute_pa_phan-loai=chinh-hang&attribute_pa_mau=black&attribute_pa_phien-ban=lens-16-50mm-f-3-5-5-6-oss-ii" ||
		o.PriceVND != 26_100_000 || o.RegularVND != 30_000_000 || o.Quota != "0/7" || o.InStock == nil || !*o.InStock ||
		!o.FlashStart.Equal(start) || !o.FlashEnd.Equal(start.Add(2*time.Hour)) {
		t.Errorf("offer = %+v", o)
	}

	// A refused request (e.g. an expired nonce) is an error, not "no offers".
	if _, err := Extract(NewBNFlash(), "https://bncamera.com/", []byte(`{"success":false,"data":{"message":"Nonce không hợp lệ"}}`)); err == nil {
		t.Error("refused request: no error")
	}
}

// Sold out: every unit on offer sold.
func TestBNFlashSoldOut(t *testing.T) {
	var r map[string]any
	if err := json.Unmarshal(readFlash(t, "flash-slot.json"), &r); err != nil {
		t.Fatal(err)
	}
	data := r["data"].(map[string]any)                                                             //nolint:forcetypeassert // fixture shape
	data["html"] = strings.Replace(data["html"].(string), "Đã bán 0/7 suất", "Đã bán 7/7 suất", 1) //nolint:forcetypeassert // fixture shape
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	offers, err := Extract(NewBNFlash(), "https://bncamera.com/", raw)
	if err != nil {
		t.Fatal(err)
	}
	if o := offers[0]; o.Quota != "7/7" || o.InStock == nil || *o.InStock {
		t.Errorf("sold-out offer = %+v", o)
	}
}

func TestVariantLabel(t *testing.T) {
	for in, want := range map[string]string{
		"https://b/san-pham/x-s20?attribute_pa_phien-ban=body-only":                        "body only",
		"https://b/san-pham/x-m5?attribute_pa_mau=silver&attribute_pa_phien-ban=body-only": "silver, body only",
		"https://b/san-pham/x-t5":              "",
		"https://b/san-pham/x-t5?utm_source=x": "",
	} {
		if got := variantLabel(in); got != want {
			t.Errorf("variantLabel(%s) = %q, want %q", in, got, want)
		}
	}
}
