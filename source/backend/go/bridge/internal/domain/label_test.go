package domain

import (
	"encoding/json"
	"testing"
)

func TestCheckLabel(t *testing.T) {
	for name, label := range map[string]string{
		"plain":            "Ana",
		"accents and ñ":    "José Núñez",
		"CJK":              "山田 太郎",
		"emoji with ZWJ":   "👨‍👩‍👧 familia",
		"RTL script alone": "مرحبا",
	} {
		if err := checkLabel("name", label); err != nil {
			t.Errorf("%s rejected: %v", name, err)
		}
	}
	for name, label := range map[string]string{
		"newline":              "Ana\nAdmin",
		"tab":                  "Ana\tB",
		"NUL":                  "A\x00na",
		"escape":               "A\x1bna",
		"RLO override":         "Ana‮odnarg",
		"LRE":                  "‪Beto",
		"RLI isolate":          "Beto⁧",
		"PDI":                  "Beto⁩",
		"line separator":       "A B",
		"paragraph separator":  "A B",
		"only zero-width":      "​‍",
		"zero-width and space": " ​ ",
		"DEL":                  "A\x7fB",
		"C1 control":           "A\u0085B",
	} {
		if err := checkLabel("name", label); err == nil {
			t.Errorf("%s accepted: %q", name, label)
		}
	}
}

func TestLabelsAreCheckedWhereTheyEnter(t *testing.T) {
	join := func(name string) error {
		_, err := ParseClientMessage([]byte(`{"type":"join","name":` + jsonString(name) + `}`))
		return err
	}
	if err := join("Ana"); err != nil {
		t.Fatal(err)
	}
	if err := join("Ana‮evil"); err == nil {
		t.Fatal("bidi override accepted in join.name")
	}
	if _, err := ParseClientMessage([]byte(`{"type":"join","name":"Ana","title":"x‮y"}`)); err == nil {
		t.Fatal("bidi override accepted in join.title")
	}
	settings := RoomSettings{Title: "Sala", Host: HostInfo{Name: "Host‮"}}
	if err := settings.Validate(); err == nil {
		t.Fatal("bidi override accepted in the host name")
	}
	settings = RoomSettings{Title: "Sala\n2", Host: HostInfo{Name: "Host"}}
	if err := settings.Validate(); err == nil {
		t.Fatal("newline accepted in the room title")
	}
	settings = RoomSettings{Title: "Sala", Host: HostInfo{Name: "Host"}}
	if err := settings.Validate(); err != nil {
		t.Fatal(err)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
