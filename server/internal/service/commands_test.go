package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
)

func testDevice(version string) *model.Device {
	remote := map[string][]string{"example.heartbeat": {"status", "set"}, "system.update": {"install"}, "net.link": {"status"}}
	rc, _ := json.Marshal(remote)
	schema := json.RawMessage(`{
	  "example.heartbeat": {
	    "status": {"label": "查询状态", "result": [{"key": "beats", "type": "int"}]},
	    "set": {"label": "开关计数", "params": [{"key": "on", "type": "bool", "label": "启用", "required": true}]},
	    "hidden": {"label": "未开放的命令"}
	  },
	  "system.update": {"install": {"label": "安装更新"}}
	}`)
	return &model.Device{ID: 1, Version: version, RemoteCommands: rc, CommandSchema: normalizeCommandSchema(schema, remote), LastSeenAt: time.Now()}
}

func TestDeviceCommands(t *testing.T) {
	cmds := DeviceCommands(testDevice("1.6.8"))
	keys := map[string]bool{}
	for _, c := range cmds {
		keys[c.Module+"/"+c.Command] = c.Declared
	}
	if len(cmds) != 3 || !keys["example.heartbeat/set"] || !keys["example.heartbeat/status"] {
		t.Fatalf("got %+v", keys)
	}
	if _, ok := keys["system.update/install"]; ok {
		t.Fatal("internal command exposed")
	}
	if keys["net.link/status"] {
		t.Fatal("net.link/status has no schema, should be undeclared")
	}
}

func TestValidateArgs(t *testing.T) {
	sp := FindCommand(testDevice("1.6.8"), "example.heartbeat", "set")
	if _, err := ValidateArgs(sp, map[string]any{"on": true}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]any{{}, {"on": "yes"}, {"on": true, "typo": 1}} {
		if _, err := ValidateArgs(sp, bad); !errors.Is(err, errcode.InvalidParams) {
			t.Fatalf("%v accepted", bad)
		}
	}
	// 没有说明的命令：原样放行。
	raw := FindCommand(testDevice("1.6.8"), "net.link", "status")
	if out, err := ValidateArgs(raw, map[string]any{"anything": 1}); err != nil || out["anything"] != 1 {
		t.Fatal("undeclared command should pass through")
	}
}

func TestCommandCatalogPrefersDeclared(t *testing.T) {
	old := testDevice("1.6.7")
	old.ID, old.CommandSchema = 2, nil
	cat := CommandCatalog([]model.Device{*old, *testDevice("1.6.8")})
	for _, c := range cat {
		if c.Module == "example.heartbeat" && c.Command == "set" {
			if !c.Declared || len(c.Devices) != 2 {
				t.Fatalf("got %+v", c)
			}
			return
		}
	}
	t.Fatal("set missing")
}

func TestCommandCatalogWindowAndOnline(t *testing.T) {
	stale := testDevice("1.6.8")
	stale.ID, stale.LastSeenAt = 3, time.Now().Add(-CatalogWindow-time.Hour) // 长期不上线：不进目录
	offline := testDevice("1.6.8")
	offline.ID = 4
	cat := CommandCatalog([]model.Device{*stale, *offline})
	if len(cat) == 0 {
		t.Fatal("recent offline device should stay in catalog")
	}
	for _, c := range cat {
		if len(c.Devices) != 1 || c.Devices[0] != 4 || c.Online {
			t.Fatalf("got %+v", c)
		}
	}
	online := testDevice("1.6.8")
	online.ID, online.Online, online.LastSeenAt = 5, true, time.Now().Add(-2*CatalogWindow) // 在线的不看时间
	for _, c := range CommandCatalog([]model.Device{*stale, *offline, *online}) {
		if !c.Online || len(c.Devices) != 2 {
			t.Fatalf("got %+v", c)
		}
	}
	if len(CommandCatalog([]model.Device{*stale})) != 0 {
		t.Fatal("stale device should be excluded")
	}
}

func TestCommandSchemaIcon(t *testing.T) {
	remote := map[string][]string{"my.mod": {"a", "b"}}
	raw := json.RawMessage(`{"my.mod": {"a": {"label": "甲", "icon": "toggle"}, "b": {"label": "乙", "icon": "<script>"}}}`)
	d := &model.Device{ID: 1, LastSeenAt: time.Now(), CommandSchema: normalizeCommandSchema(raw, remote)}
	d.RemoteCommands, _ = json.Marshal(remote)
	icons := map[string]string{}
	for _, c := range DeviceCommands(d) {
		icons[c.Command] = c.Icon
	}
	if icons["a"] != "toggle" || icons["b"] != "" {
		t.Fatalf("icons %v", icons)
	}
}
