package snapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://fujifilm-xspace.vn/products/fujinon-xf10-24mmf4-r-ois-wr-ii", "fujinon-xf10-24mmf4-r-ois-wr-ii"},
		{"https://vjshop.vn/do-cu-chat", "do-cu-chat"},
		{"https://mayanhhoangto.com/ong-kinh/ong-kinh-fuji/", "ong-kinh-fuji"},
		{"https://vjshop.vn/may-anh-fujifilm?page=2", "may-anh-fujifilm-page-2"},
		{"https://bncamera.com/?s=x-t5&post_type=product", "s-x-t5-post_type-product"},
		{"https://kyma.vn/", "index"},
		{"https://zshop.vn/fujifilm-x-t5.html", "fujifilm-x-t5"},
		{"https://kyma.vn/may-anh-fujifilm-x-t5.html?product-id=163637", "may-anh-fujifilm-x-t5-product-id-163637"},
		{"https://shop.vn/index.php", "index"},
		{"https://kyma.vn/Máy Ảnh", "m-y-nh"},
		// Long names are cut and keep a hash of the URL.
		{"https://b.vn/wp-json/wc/store/v1/products?parent=33775%2C29365%2C1%2C2%2C3%2C4%2C5%2C6%2C7%2C8%2C9%2C10&per_page=100&type=variation",
			"products-parent-33775-2c29365-2c1-2c2-2c3-2c4-2c5-2c6-2c7-2c8-2c9-2c10-5b5a17c7"},
	}
	for _, tt := range tests {
		if got := Name(tt.in); got != tt.want {
			t.Errorf("Name(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExt(t *testing.T) {
	if Ext([]byte("  {\"a\":1}")) != ".json" || Ext([]byte("[1]")) != ".json" {
		t.Error("JSON body not detected")
	}
	if Ext([]byte("<!doctype html>")) != ".html" || Ext(nil) != ".html" {
		t.Error("HTML body not detected")
	}
}

// docs/usage.md, Adding a shop, step 2: snapshot saves testdata/<shop>/<slug>.html.
func TestSaveAndLoadMeta(t *testing.T) {
	dir := t.TempDir()
	m := Meta{
		Shop:      "xspace",
		Mode:      "product",
		PageURL:   "https://fujifilm-xspace.vn/products/x-t5",
		FetchURL:  "https://fujifilm-xspace.vn/products/x-t5.js",
		FetchedAt: time.Date(2026, 11, 7, 23, 17, 0, 0, time.UTC),
	}
	path, err := Save(dir, m, []byte(`{"title":"X-T5"}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "xspace", "x-t5.json"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "xspace", "x-t5"+MetaSuffix)); err != nil {
		t.Errorf("meta file not written: %v", err)
	}
	got, err := LoadMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != m {
		t.Errorf("LoadMeta = %+v, want %+v", got, m)
	}

	htmlPath, err := Save(dir, Meta{Shop: "vjshop", PageURL: "https://vjshop.vn/do-cu-chat"}, []byte("<html></html>"))
	if err != nil || htmlPath != filepath.Join(dir, "vjshop", "do-cu-chat.html") {
		t.Errorf("html path = %q, %v", htmlPath, err)
	}
	if _, err := LoadMeta(filepath.Join(dir, "missing.html")); err == nil {
		t.Error("LoadMeta on missing file: expected error")
	}
}
