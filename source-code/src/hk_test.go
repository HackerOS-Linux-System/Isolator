package src

import "testing"

func TestHKBasic(t *testing.T) {
	src := `! To jest komentarz

[metadata]
-> name    => Hacker Lang
-> version => 1.5
-> stable  => true
-> tags    => ["lang", "compiler", "hacker"]

[build]
-> output  => "./dist"
-> target  => linux-x86_64
-> debug   => false
`
	doc, err := ParseHK(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	meta := doc.Section("metadata")
	name, _ := meta.Get("name")
	if s, _ := name.AsString(); s != "Hacker Lang" {
		t.Errorf("expected 'Hacker Lang', got %q", s)
	}
	ver, _ := meta.Get("version")
	if n, _ := ver.AsNumber(); n != 1.5 {
		t.Errorf("expected 1.5, got %v", n)
	}
	stable, _ := meta.Get("stable")
	if b, _ := stable.AsBool(); !b {
		t.Errorf("expected stable=true")
	}
	tags, _ := meta.Get("tags")
	arr, _ := tags.AsArray()
	if len(arr) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(arr))
	}

	build := doc.Section("build")
	output, _ := build.Get("output")
	if s, _ := output.AsString(); s != "./dist" {
		t.Errorf("expected './dist', got %q", s)
	}
}

func TestHKNestingDashStyle(t *testing.T) {
	src := `[libraries]
-> obsidian
--> version     => 0.2
--> description => Biblioteka inspirowana zenity.
--> authors     => ["HackerOS Team <hackeros068@gmail.com>"]

-> yuy
--> version     => 0.2
--> description => Twórz ładne interfejsy CLI
`
	doc, err := ParseHK(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	libs := doc.Section("libraries")
	obsV, ok := libs.Get("obsidian")
	if !ok || obsV.Kind != HkMapKind {
		t.Fatalf("expected 'obsidian' submap")
	}
	ver, _ := obsV.MapVal.Get("version")
	if n, _ := ver.AsNumber(); n != 0.2 {
		t.Errorf("expected 0.2, got %v", n)
	}
	yuyV, ok := libs.Get("yuy")
	if !ok || yuyV.Kind != HkMapKind {
		t.Fatalf("expected 'yuy' submap")
	}
	desc, _ := yuyV.MapVal.Get("description")
	if s, _ := desc.AsString(); s != "Twórz ładne interfejsy CLI" {
		t.Errorf("unexpected description: %q", s)
	}
}

func TestHKDottedKeys(t *testing.T) {
	src := `[config]
-> database.host => localhost
-> database.port  => 5432
-> database.ssl   => true
-> server.tls.cert => /etc/ssl/cert.pem
-> server.tls.key  => /etc/ssl/key.pem
`
	doc, err := ParseHK(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := doc.Section("config")
	dbV, ok := cfg.Get("database")
	if !ok || dbV.Kind != HkMapKind {
		t.Fatalf("expected 'database' map from dotted keys")
	}
	host, _ := dbV.MapVal.Get("host")
	if s, _ := host.AsString(); s != "localhost" {
		t.Errorf("expected localhost, got %q", s)
	}
	srv, _ := cfg.Get("server")
	tls, _ := srv.MapVal.Get("tls")
	cert, _ := tls.MapVal.Get("cert")
	if s, _ := cert.AsString(); s != "/etc/ssl/cert.pem" {
		t.Errorf("expected cert path, got %q", s)
	}
}

func TestHKInterpolation(t *testing.T) {
	src := `[project]
-> name     => MyApp
-> version  => 1.0

[paths]
-> base     => /opt/${project.name}
-> config   => ${paths.base}/config

[data]
-> numbers  => [10, 20, 30]
-> first    => ${data.numbers[0]}
`
	doc, err := ParseHK(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ResolveInterpolations(doc); err != nil {
		t.Fatalf("unexpected interpolation error: %v", err)
	}
	paths := doc.Section("paths")
	base, _ := paths.Get("base")
	if s, _ := base.AsString(); s != "/opt/MyApp" {
		t.Errorf("expected '/opt/MyApp', got %q", s)
	}
	cfgPath, _ := paths.Get("config")
	if s, _ := cfgPath.AsString(); s != "/opt/MyApp/config" {
		t.Errorf("expected '/opt/MyApp/config', got %q", s)
	}
	data := doc.Section("data")
	first, _ := data.Get("first")
	if s, _ := first.AsString(); s != "10" {
		t.Errorf("expected '10', got %q", s)
	}
}

func TestHKCyclicReference(t *testing.T) {
	src := `[a]
-> x => ${b.y}

[b]
-> y => ${a.x}
`
	doc, err := ParseHK(src)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if err := ResolveInterpolations(doc); err == nil {
		t.Fatalf("expected a cyclic reference error")
	}
}

func TestHKRoundTrip(t *testing.T) {
	src := `[server]
-> host    => localhost
-> port    => 8080
-> ssl     => true
-> tls
--> cert  => /etc/cert.pem
--> key   => /etc/key.pem
-> allowed => ["GET", "POST"]
`
	doc, err := ParseHK(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := SerializeHK(doc)
	doc2, err := ParseHK(out)
	if err != nil {
		t.Fatalf("re-parse of serialized output failed: %v\n--- output ---\n%s", err, out)
	}
	srv := doc2.Section("server")
	host, _ := srv.Get("host")
	if s, _ := host.AsString(); s != "localhost" {
		t.Errorf("round-trip lost 'host': got %q", s)
	}
	tls, _ := srv.Get("tls")
	cert, _ := tls.MapVal.Get("cert")
	if s, _ := cert.AsString(); s != "/etc/cert.pem" {
		t.Errorf("round-trip lost nested 'tls.cert': got %q", s)
	}
}

func TestHKKeyConflict(t *testing.T) {
	src := `[a]
-> x => 1
-> x.y => 2
`
	_, err := ParseHK(src)
	if err == nil {
		t.Fatalf("expected KeyConflict error")
	}
}
