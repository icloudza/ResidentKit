package open

import (
	"encoding/json"
	"testing"

	"github.com/icloudza/ResidentKit/server/internal/dto"
)

func TestBuildSpec(t *testing.T) {
	cat := []dto.CommandSpec{{Module: "example.heartbeat", Command: "set", Label: "开关计数", Declared: true,
		Params: []dto.CommandParam{{ConfigItem: dto.ConfigItem{Key: "on", Type: "bool"}, Required: true}},
		Result: []dto.CommandField{{Key: "beats", Type: "int"}}}}
	var doc map[string]any
	if err := json.Unmarshal(BuildSpec(cat), &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	op, ok := paths["/devices/{id}/commands/example.heartbeat/set"].(map[string]any)
	if !ok || op["post"] == nil {
		t.Fatalf("command path missing: %v", paths)
	}
	body := op["post"].(map[string]any)["requestBody"].(map[string]any)
	schema := body["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if schema["required"].([]any)[0] != "on" {
		t.Fatalf("required missing: %v", schema)
	}
}
