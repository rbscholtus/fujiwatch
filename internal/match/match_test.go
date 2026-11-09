package match

import (
	"slices"
	"sync"
	"testing"

	"github.com/rbscholtus/fujiwatch/internal/config"
)

func TestNormalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Ống kính Fujinon XF10-24mm F4 R OIS WR II", "ong kinh fujinon xf10-24mm f4 r ois wr ii"},
		{"Máy ảnh Fujifilm XT5 (Đen)", "may anh fujifilm x-t5 (den)"},
		{"Fujifilm X T3 body", "fujifilm x-t3 body"},
		{"Fujifilm X-T3", "fujifilm x-t3"},
		{"FUJIFILM X-T30 II", "fujifilm x-t30 ii"},
		{"Fujifilm X-H2S", "fujifilm x-h2s"},
		{"Fujifilm X Pro3", "fujifilm x-pro3"},
		{"Tamron 16–50mm f/2.8 Di III VXD", "tamron 16-50mm f2.8 di iii vxd"},
		{"Tamron 16 - 50mm F / 2.8", "tamron 16-50mm f2.8"},
		{"XC15-45mm", "xc15-45mm"},
		{"  Đồ   cũ chất  ", "do cu chat"},
		{"Ống kính đã qua sử dụng", "ong kinh da qua su dung"},
	}
	for _, tt := range tests {
		if got := Normalize(tt.in); got != tt.want {
			t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func repoMatcher(t *testing.T) *Matcher {
	t.Helper()
	cfg, err := config.Load("../../config/watchlist.yaml", "../../config/sources.yaml")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	m, err := New(cfg.Watchlist.Products)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// TestMatchWatchlist runs real-looking shop titles through the repo's
// watchlist.yaml. "" means the title must not match anything.
func TestMatchWatchlist(t *testing.T) {
	m := repoMatcher(t)

	tests := []struct {
		title string
		want  string
	}{
		// Add-on variations: the price is the lens bought with the body
		{"Máy ảnh FUJIFILM X-E5 BODY (XE5 Mới chính hãng ) (Phân loại: Len fuji 23 2.8 mua cùng)", ""},
		{"Máy ảnh FUJIFILM X-E5 BODY (XE5 Mới chính hãng ) (Phân loại: Len Sigma 18-50 mua cùng)", ""},
		{"Máy ảnh FUJIFILM X-E5 BODY (XE5 Mới chính hãng ) (Phân loại: Hàng mới chính hãng)", "x-e5"},
		{"Máy ảnh FUJIFILM X-T5 (Màu sắc: Black, Biến thể: Body only)", "x-t5"},
		{"Máy ảnh FUJIFILM X-T5 (Màu sắc: Silver, Biến thể: + Lens XF 16-50mm f/2.8-4.8)", ""},
		// Dual-mount Sigma listings are X-mount offers too
		{"Ống kính Sigma 10-18mm f/2.8 DC DN Contemporary for Fujifilm/Sony", "sigma-10-18"},
		{"Ống kính Sigma 18-50mm f/2.8 DC DN for Sony E/Fujifilm", "sigma-18-50"},
		{"Sigma 10-18mm f/2.8 DC DN (C) For Sony E / Fujifilm XF", "sigma-10-18"},
		{"Sigma 18-50mm f/2.8 DC DN (C) For Sony E", ""},
		// X-T6 (not in shops yet) vs a future X-T60, and kits
		{"Máy ảnh Fujifilm X-T6 Body | Chính hãng", "x-t6"},
		{"Fujifilm XT6 (Silver)", "x-t6"},
		{"Fujifilm X-T60 Body", ""},
		{"Fujifilm X-T6 Kit XF16-50mm", ""},
		// Other brands' 10-24 zooms ("Di II" contains "ii")
		{"TAMRON 10-24mm f/3.5-4.5 Di II VC HLD", ""},
		{"Tamron SP AF 10-24mm f/3.5-4.5 DI II Zoom for Canon/ Nikon", ""},
		{"Fujifilm XF 10-24mm f/4 mark II", "xf10-24-ii"},
		{"Fujifilm XF 10-24mm f/4 R OIS", "xf10-24-i"},
		// Cine lenses are not the XF18-55
		{"Fujifilm MKX18-55mm T2.9", ""},
		{"Fujifilm MK X 18-55mm T2.9 (Chính hãng)", ""},
		// X-T3 vs X-T30
		{"Máy ảnh Fujifilm X-T3 Body (Chính hãng)", "x-t3"},
		{"Fujifilm XT3 body cũ 98%", "x-t3"},
		{"Fujifilm X T3 + kit XF18-55mm", ""}, // kit, not body-only
		{"Fujifilm X-T3", "x-t3"},
		{"Fujifilm X-T3 kèm grip, dùng chung pin với X-T30", "x-t3"},
		{"Máy ảnh Fujifilm X-T30", ""},
		{"Fujifilm X-T30 II Body", "x-t30-ii"},
		{"Fujifilm XT30 III kit XC15-45mm", ""},
		{"Máy ảnh Fujifilm X-T30 mark 2 (Body XT30 mark II) (Phân loại: Hàng Đẹp)", "x-t30-ii"},
		{"Máy ảnh FUJIFILM X-T30 II (Bảo hành: 24 tháng X-Camera, Màu sắc: Black, Biến thể: Body only)", "x-t30-ii"},
		{"Máy ảnh FUJIFILM X-T30 II (Bảo hành: 24 tháng X-Camera, Màu sắc: Black, Biến thể: + Lens XC 15-45mm F/3.5-5.6)", ""},
		{"Fujifilm XT30II cũ", "x-t30-ii"},
		{"Máy ảnh FUJIFILM X-T30 III (Biến thể: Body, Màu sắc: Silver)", "x-t30-iii"},
		{"Máy ảnh FUJIFILM X-T30 III (XT30 3 – Mới) (Phân loại: Mới nhập khẩu)", "x-t30-iii"},
		{"Fujifilm X-T30 Mark III", "x-t30-iii"},
		{"Máy ảnh FUJIFILM X-T30 III (Biến thể: + Sigma 18-50mm f2.8, Màu sắc: Black)", ""},

		// X-T4
		{"Fujifilm X-T4 Body Silver", "x-t4"},
		{"Fujifilm XT4 like new", "x-t4"},

		// X-T5 vs X-T50
		{"Máy ảnh Fujifilm X-T5 Body (Đen)", "x-t5"},
		{"Fujifilm XT5 kit XF16-80mm", ""}, // kit, not body-only
		{"Máy ảnh Fujifilm X-T50 Body", "x-t50"},
		{"Fujifilm XT50 kit XC15-45", ""},
		{"Máy ảnh Fujifilm X-T50 (Black, Body Only) | Chính hãng", "x-t50"},
		{"Fujifilm X-T50 cũ (7337)", "x-t50"},
		{"Ho Cheng Wei – X-T50 + XF16-50", ""},
		{"Máy ảnh Fujifilm X-T30 II 2nd 98% (#05922) + XF 23mmF2", ""},
		{"Máy ảnh Fujifilm X-T50 + Lens XF 16-50mm f/2.8-4.8 (Silver) | Chính hãng", ""},

		// Kits never match body-only products (real titles from the snapshots).
		{"Máy ảnh Fujifilm X-T5 + Lens XF 16-50mm F/2.8-4.8 (Black) | Chính Hãng", ""},
		{"Fujifilm X-T5 Kit XF 16-80mm F4 (Màu Bạc) - Mới 100% (Chính hãng)", ""},
		{"Fujifilm X-T5 + Tamron 17-70mm f/2.8 (Màu Bạc), Mới 100% (Chính Hãng)", ""},
		{"Fujifilm X-T5 + XF 16-50mm F2.8-4.8 (Bạc), Mới 100% (Chính Hãng)", ""},
		{"Máy ảnh Fujifilm X-T3 + ống kính", ""},
		// Body-only titles still match.
		{"Fujifilm X-T5 (Chính hãng) (Body Only, Black)", "x-t5"},
		{"Máy ảnh Fujifilm X-T5 (Body, Bạc)", "x-t5"},
		{"Fujifilm X-T4 Body Black (85% - BH 03 Tháng) HN", "x-t4"},
		{"Máy ảnh FUJIFILM X-T3 (XT3) Body cũ", "x-t3"},
		{"Fujifilm X-T3 cũ | 2nd (0831)", "x-t3"},

		// XF10-24 Mark I vs Mark II
		{"Ống kính Fujinon XF10-24mm F4 R OIS WR II", "xf10-24-ii"},
		{"Fujifilm XF 10-24mm f/4 R OIS WR", "xf10-24-ii"},
		{"Fujinon XF10-24mm F4 R OIS Mark II", "xf10-24-ii"},
		{"Ống kính Fujifilm XF10-24mm F4 R OIS", "xf10-24-i"},
		{"Fujinon XF10–24mm F/4 R OIS cũ", "xf10-24-i"},

		// Tamron X-mount vs other mounts
		{"Ống kính Tamron 16-50mm F2.8 Di III VXD cho Fujifilm X", "tamron-16-50"},
		{"Tamron 16-50mm f/2.8 Di III VXD (Fujifilm X-mount)", "tamron-16-50"},
		{"Tamron 16-50mm F2.8 Di III VXD ngàm X", "tamron-16-50"},
		{"Tamron 16-50mm F2.8 Di III VXD for Sony E-mount", ""},
		{"Tamron 16-50mm F2.8 Di III VXD Sony FE", ""},
		{"Tamron 16-50mm F2.8 Di III VXD Nikon Z", ""},
		{"Tamron 16-50mm F2.8 Di III VXD", ""}, // mount unknown → don't guess

		// Sigma 10-18 and 18-50 (X-mount): real titles from the snapshots.
		{"Ống kính Sigma 10-18mm f/2.8 DC DN Contemporary for Fujifilm/Sony", "sigma-10-18"},
		{"Sigma 18-50mm f/2.8 DC DN for Fujifilm X", "sigma-18-50"},
		{"Ống kính Sigma 18-50mm f/2.8 DC DN for Sony E/Fujifilm", "sigma-18-50"}, // dual-mount listing
		{"Ống kính Sigma 18-50mm f/2.8 DC DN (C) For Sony E 2nd (#13013)", ""},    // Sony only
		{"Sigma 18-50mm F2.8 DC DN Contemporary L-Mount", ""},
		{"Sigma 10-18mm F2.8 DC DN Contemporary ngàm X", "sigma-10-18"},
		{"Fujifilm X-T5 + Sigma 18-50mm f/2.8 XF (Màu Bạc), Mới 100% (Chính Hãng)", ""}, // kit
		{"Sigma 18-35mm F1.8 DC HSM Art", ""},
		{"Sigma 10-20mm F3.5 EX DC HSM", ""},

		// Fujifilm XF18-55mm F2.8-4 R LM OIS.
		{"Ống kính Fujifilm XF18-55mm F2.8-4 R LM OIS", "xf18-55"},
		{"Fujinon XF 18-55mm f/2.8-4 R LM OIS cũ 98%", "xf18-55"},
		{"FUJIFILM XF 18-55mm f/2.8-4 R LM OIS (Like new)", "xf18-55"},
		{"Fujifilm X T3 + kit XF18-55mm", ""},                    // kit
		{"Canon EF-S 18-55mm f/3.5-5.6 IS STM", ""},              // other brand
		{"Nikon AF-P DX 18-55mm f/3.5-5.6G VR", ""},              // other brand
		{"Sony E 18-55mm F3.5-5.6 OSS cho Fujifilm? (nhầm)", ""}, // other brand
		{"Fujifilm XC 15-45mm F3.5-5.6 OIS PZ", ""},

		// Lens kits: a lens product never takes a body-plus-lens price.
		{"Fujifilm X-T5 + Tamron 16-50mm F2.8 Di III VXD", ""},
		{"Combo Fujifilm X-T4 + Fujinon XF10-24mm F4 R OIS WR", ""},

		// Unrelated
		{"Fujinon XF18-55mm F2.8-4 R LM OIS", "xf18-55"}, // watched since it was added
		{"Fujifilm X100VI", ""},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			res := m.Match(tt.title)
			if tt.want == "" {
				if res.Status != Unmatched {
					t.Errorf("Match(%q) = %s %v, want unmatched (normalised %q)",
						tt.title, res.Status, res.Candidates, res.Normalized)
				}
				return
			}
			if res.Status != Matched || res.ProductID != tt.want {
				t.Errorf("Match(%q) = %s %v, want %s (normalised %q)",
					tt.title, res.Status, res.Candidates, tt.want, res.Normalized)
			}
		})
	}
}

func TestMatchAmbiguous(t *testing.T) {
	m := repoMatcher(t)
	// One listing for two bodies: neither product may claim it.
	res := m.Match("Fujifilm X-T4 / X-T5 body (thu cũ đổi mới)")
	if res.Status != Ambiguous {
		t.Fatalf("Status = %s, want ambiguous", res.Status)
	}
	if !slices.Equal(res.Candidates, []string{"x-t4", "x-t5"}) {
		t.Errorf("Candidates = %v", res.Candidates)
	}
	if res.ProductID != "" {
		t.Errorf("ProductID = %q, want empty for ambiguous", res.ProductID)
	}
}

// A near miss names a watched product without confirming its mount or
// version; a deliberate rejection (exclude: kits, other mounts) is not one.
func TestNearMiss(t *testing.T) {
	m := repoMatcher(t)
	for _, tc := range []struct {
		title string
		want  []string
	}{
		{"Ống kính Tamron 16-50mm F2.8 Di III VXD", []string{"tamron-16-50"}}, // mount not stated
		{"Ống kính Sigma 10-18mm f/2.8 DC DN", []string{"sigma-10-18"}},       // no mount stated
		{"Ống kính Sigma 18-50mm f/2.8 DC DN (C) For Sony E", nil},            // another mount (other_mounts)
		{"Sigma 18-50mm F2.8 DC DN (C) For Canon RF", nil},
		{"Ống kính Sigma 18-50mm f/2.8 DC DN (C) ngàm Canon RF (18 50 – Mới)", nil},
		{"Ống Kính Sigma 10-18mm F2.8 DC DN Contemporary (E-Mount) | Chính Hãng", nil},
		{"Sigma 10-18mm f/2.8 DC DN (C) Canon RF-S (Chính hãng)", nil},
		{"Sigma 18-50mm f/2.8 DC DN Contemporary for L-Mount", nil},
		{"Ống kính Tamron 16-50mm F2.8 Di III VXD for Sony E", nil},             // excluded mount
		{"Máy ảnh Fujifilm X-T5 + Lens XF 16-50mm F/2.8-4.8 | Chính Hãng", nil}, // kit
		{"Máy ảnh Fujifilm X-H2 | Chính hãng", nil},                             // another model
		{"Ống kính Fujifilm XF 16-55mm f/2.8 R LM WR | Chính hãng", nil},        // matched, so no near miss
	} {
		if got := m.Match(tc.title).NearMiss; !slices.Equal(got, tc.want) {
			t.Errorf("NearMiss(%q) = %v, want %v", tc.title, got, tc.want)
		}
	}
}

func TestExplain(t *testing.T) {
	m := repoMatcher(t)
	_, traces := m.Explain("Tamron 16-50mm F2.8 Di III VXD Sony E (Fujifilm version also in stock)")
	i := slices.IndexFunc(traces, func(tr Trace) bool { return tr.ProductID == "tamron-16-50" })
	if i < 0 {
		t.Fatal("no trace for tamron-16-50")
	}
	if tr := traces[i]; tr.Matched || tr.Reason != `exclude 'sony' matches` {
		t.Errorf("trace = %+v", tr)
	}
}

func TestNewInvalidPattern(t *testing.T) {
	_, err := New([]config.Product{{ID: "bad", Patterns: []string{"("}}})
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

func TestDetectCondition(t *testing.T) {
	tests := []struct {
		title, url, want string
	}{
		{"Fujifilm X-T4 cũ 98%", "", ConditionUsed},
		{"Fujifilm X-T4 Body", "https://vjshop.vn/do-cu-chat/fujifilm-x-t4", ConditionUsed},
		{"Fujifilm X-T4 like new", "", ConditionUsed},
		{"Fujifilm X-T4 likenew fullbox", "", ConditionUsed},
		{"Fujifilm X-T4 95%", "", ConditionUsed},
		{"Fujifilm X-T4 đã qua sử dụng", "", ConditionUsed},
		{"Fujifilm X-T4 2nd", "", ConditionUsed},
		{"Fujifilm X-T4 Used", "", ConditionUsed},
		{"Fujifilm X-T4 Body Chính hãng", "https://shop.vn/fujifilm-x-t4", ConditionNew},
		{"Fujifilm X-T4 giảm 10%", "", ConditionNew},
		{"Ống kính Fujinon XF10-24mm", "https://shop.vn/ong-kinh-cuc-net", ConditionNew},
	}
	for _, tt := range tests {
		if got := DetectCondition(tt.title, tt.url); got != tt.want {
			t.Errorf("DetectCondition(%q, %q) = %q, want %q", tt.title, tt.url, got, tt.want)
		}
	}
}

// Normalize is called from concurrently running shops.
func TestNormalizeConcurrent(t *testing.T) {
	const title, want = "Ống kính Fujinon XF10–24mm F/4 R OIS WR", "ong kinh fujinon xf10-24mm f4 r ois wr"
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				if got := Normalize(title); got != want {
					t.Errorf("Normalize = %q", got)
					return
				}
			}
		})
	}
	wg.Wait()
}
