package media

import (
	"testing"
)

func TestPublicIDFromURL(t *testing.T) {
	cases := map[string]string{
		"https://res.cloudinary.com/acme/image/upload/v123/rallya/events/org/ev-ab12.jpg": "rallya/events/org/ev-ab12",
		"https://res.cloudinary.com/acme/image/upload/rallya/events/org/ev-ab12.png":      "rallya/events/org/ev-ab12",
		"http://res.cloudinary.com/acme/image/upload/v9/a/b.webp?x=1":                     "a/b",
	}
	for in, want := range cases {
		got, ok := PublicIDFromURL(in, "acme")
		if !ok || got != want {
			t.Fatalf("PublicIDFromURL(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	bad := []string{
		"",
		"https://example.com/x.jpg",
		"https://res.cloudinary.com/other/image/upload/a.jpg",
		"https://res.cloudinary.com/acme/image/upload/../../etc.jpg",
		"https://res.cloudinary.com/acme/image/upload/",
		"/uploads/events/org/x.jpg",
	}
	for _, in := range bad {
		if got, ok := PublicIDFromURL(in, "acme"); ok {
			t.Fatalf("PublicIDFromURL(%q) = %q, want reject", in, got)
		}
	}
	if _, ok := PublicIDFromURL("https://res.cloudinary.com/acme/image/upload/a.jpg", ""); ok {
		t.Fatal("empty cloud must reject")
	}
}

func TestCloudNameFromURL(t *testing.T) {
	got, ok := CloudNameFromURL("cloudinary://key:secret@mycloud")
	if !ok || got != "mycloud" {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, ok := CloudNameFromURL("not-a-url"); ok {
		t.Fatal("expected reject")
	}
}
