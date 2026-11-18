package service

import (
	"encoding/json"
	"testing"

	"github.com/icloudza/ResidentKit/server/internal/dto"
)

func TestNormalizeValue(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	intItem := dto.ConfigItem{Key: "interval_s", Type: "int", Min: f(10), Max: f(600), Default: 30.0, Label: "x"}
	cases := []struct {
		it dto.ConfigItem
		v  any
		ok bool
	}{
		{intItem, 30.0, true},
		{intItem, 9.0, false},
		{intItem, 30.5, false},
		{intItem, "30", false},
		{dto.ConfigItem{Type: "bool"}, true, true},
		{dto.ConfigItem{Type: "bool"}, 1.0, false},
		{dto.ConfigItem{Type: "enum", Options: []string{"a", "b"}}, "b", true},
		{dto.ConfigItem{Type: "enum", Options: []string{"a", "b"}}, "c", false},
		{dto.ConfigItem{Type: "string", MaxLen: 3}, "中文字", true},
		{dto.ConfigItem{Type: "string", MaxLen: 3}, "abcd", false},
		{dto.ConfigItem{Type: "weird"}, 1.0, false},
	}
	for i, c := range cases {
		if _, err := normalizeValue(c.it, c.v); (err == nil) != c.ok {
			t.Errorf("case %d: %v → err=%v", i, c.v, err)
		}
	}
}

func TestNormalizeSchemaDropsBadItems(t *testing.T) {
	raw := json.RawMessage(`{"m":[
		{"key":"a","type":"int","default":5,"min":1,"max":10,"label":"A"},
		{"key":"b","type":"int","default":50,"min":1,"max":10,"label":"默认值越界"},
		{"key":"","type":"bool","default":true,"label":"无 key"}
	],"empty":[]}`)
	var s dto.ConfigSchema
	if err := json.Unmarshal(normalizeSchema(raw), &s); err != nil {
		t.Fatal(err)
	}
	if len(s) != 1 || len(s["m"]) != 1 || s["m"][0].Key != "a" {
		t.Fatalf("got %+v", s)
	}
	if normalizeSchema(json.RawMessage(`"junk"`)) != nil {
		t.Fatal("junk should be nil")
	}
}
