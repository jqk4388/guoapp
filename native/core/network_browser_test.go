package core

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func writeFingerprintModule(t *testing.T, data, id, manifest string) {
	t.Helper()
	directory := filepath.Join(data, moduleDirectory, id)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, moduleManifest), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, moduleEntry), []byte(`module.exports = {};`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserFingerprintFollowsLoadedModules(t *testing.T) {
	data := t.TempDir()
	manifest := `{"id":"fingerprinttest","name":"指纹测试","version":"1.0.0","runtime":"js","api":1,"adult":false,
"hosts":["fp.example.invalid","www.fp.example.invalid","*.cdn.example.invalid"],"aliases":["www.fp.example.invalid"],
"baseUrl":"https://fp.example.invalid","browserFingerprint":true}`
	writeFingerprintModule(t, data, "fingerprinttest", manifest)
	engine, err := newNativeEngine(data, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer Unregister("fingerprinttest")
	transport := engine.downloader.client.Transport.(*huangguoBrowserTransport)
	for _, host := range []string{"fp.example.invalid", "www.fp.example.invalid", "fp.example.invalid:443", "media.cdn.example.invalid"} {
		if source := transport.source(&url.URL{Scheme: "https", Host: host}); source != "fingerprinttest" {
			t.Fatalf("%s 未走浏览器指纹，得到 %q", host, source)
		}
	}
	if source := transport.source(&url.URL{Scheme: "https", Host: "other.example.invalid"}); source != "" {
		t.Fatalf("未声明的域名走了浏览器指纹：%q", source)
	}
	Unregister("fingerprinttest")
	if source := transport.source(&url.URL{Scheme: "https", Host: "fp.example.invalid"}); source != "" {
		t.Fatalf("卸载后的模块仍走浏览器指纹：%q", source)
	}
}

func TestBrowserFingerprintSharesDeclaredHostsAcrossModules(t *testing.T) {
	data := t.TempDir()
	writeFingerprintModule(t, data, "fingerprintone", `{"id":"fingerprintone","name":"指纹一号","version":"1.0.0","runtime":"js","api":1,"adult":false,
"hosts":["one.example.invalid","share.example.invalid"],"aliases":["one.example.invalid"],
"baseUrl":"https://one.example.invalid","browserFingerprint":true}`)
	writeFingerprintModule(t, data, "fingerprinttwo", `{"id":"fingerprinttwo","name":"指纹二号","version":"1.0.0","runtime":"js","api":1,"adult":false,
"hosts":["two.example.invalid","share.example.invalid"],"aliases":["two.example.invalid"],
"baseUrl":"https://two.example.invalid","browserFingerprint":true}`)
	engine, err := newNativeEngine(data, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer Unregister("fingerprintone")
	defer Unregister("fingerprinttwo")
	if _, taken := registeredSource("share.example.invalid"); taken {
		t.Fatal("共享的嵌入域名不应占用站源标识")
	}
	transport := engine.downloader.client.Transport.(*huangguoBrowserTransport)
	if source := transport.source(&url.URL{Scheme: "https", Host: "share.example.invalid"}); source != "fingerprintone" {
		t.Fatalf("共享的嵌入域名没有走浏览器指纹：%q", source)
	}
	for host, want := range map[string]string{"one.example.invalid": "fingerprintone", "two.example.invalid": "fingerprinttwo"} {
		if source := transport.source(&url.URL{Scheme: "https", Host: host}); source != want {
			t.Fatalf("%s 走到的站源是 %q", host, source)
		}
	}
}
