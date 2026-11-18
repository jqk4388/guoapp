package core

import "testing"

func TestModuleAllowedAddress(t *testing.T) {
	closed := &jsProvider{module: &jsModule{manifest: ModuleManifest{ID: "closedtest", Hosts: []string{"closed.example"}}}}
	if err := closed.allowedAddress("https://closed.example/page"); err != nil {
		t.Fatalf("已声明的域名被拒绝：%v", err)
	}
	if err := closed.allowedAddress("https://other.example/page"); err == nil {
		t.Fatal("未声明的域名没有被拒绝")
	}
	open := &jsProvider{module: &jsModule{manifest: ModuleManifest{ID: "opentest", Hosts: []string{"closed.example"}, AllowAnyHost: true}}}
	for _, address := range []string{"https://other.example/page", "https://rotating-player.example/e/abc", "http://media.example/x.mp4"} {
		if err := open.allowedAddress(address); err != nil {
			t.Fatalf("允许任意域名时仍然被拒绝：%s %v", address, err)
		}
	}
	if err := open.allowedAddress("file:///etc/passwd"); err == nil {
		t.Fatal("非 http(s) 地址没有被拒绝")
	}
}
