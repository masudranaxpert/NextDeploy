package angie

import (
	"testing"

	"panel/internal/proxy"
)

func TestSlotUpstreamDeclaresBothSlots(t *testing.T) {
	dst := map[string]proxy.UpstreamRef{}
	addDockerUpstream(dst, &proxy.UpstreamRef{Name: "nd_shop_app_3000_blue", Port: 3000})
	if _, ok := dst["nd_shop_app_3000_green"]; !ok {
		t.Fatalf("green upstream missing: %v", dst)
	}
	addDockerUpstream(dst, &proxy.UpstreamRef{Name: "nd_api_web_80", Port: 80})
	if len(dst) != 3 {
		t.Fatalf("an upstream without a slot must not get a sibling: %v", dst)
	}
	files, err := New().Render(&proxy.State{Sites: []proxy.Site{{
		ID: "app_shop_dom_1", AppID: "shop", Hosts: []string{"shop.example.com"}, Kind: proxy.SiteKindAppProxy,
		TLS:     proxy.TLSNone,
		Default: &proxy.Handler{Kind: proxy.HandlerProxy, Upstream: &proxy.UpstreamRef{Name: "nd_shop_app_3000_green", Port: 3000}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"upstreams/nd_shop_app_3000_green.conf", "upstreams/nd_shop_app_3000_blue.conf"} {
		if _, ok := files[f]; !ok {
			t.Fatalf("%s not rendered; files: %v", f, keys(files))
		}
	}
}

func keys(f proxy.Files) []string {
	var out []string
	for k := range f {
		out = append(out, k)
	}
	return out
}
