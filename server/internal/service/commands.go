package service

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"sort"
	"time"

	"gorm.io/datatypes"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/model"
	"github.com/icloudza/ResidentKit/server/internal/pkg/errcode"
	"github.com/icloudza/ResidentKit/server/internal/release"
)

// 远程命令说明（设备模块的 +commandSchema，随 hello 上报）：开放 API 据此为每个命令
// 生成独立接口与文档，并在下发前校验参数。

var iconRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

var paramTypes = []string{"bool", "int", "number", "string", "enum", "object", "array"}

type rawCommandSpec struct {
	Label   string             `json:"label"`
	Icon    string             `json:"icon,omitempty"` // 控制台图标名，见 RKModule.h
	Desc    string             `json:"desc"`
	Timeout int                `json:"timeout,omitempty"` // 秒
	Params  []dto.CommandParam `json:"params"`
	Result  []dto.CommandField `json:"result"`
}

// normalizeCommandSchema 只保留设备确实开放的命令、结构合法的说明，避免把任意 JSON 存库。
func normalizeCommandSchema(raw json.RawMessage, remote map[string][]string) datatypes.JSON {
	var in map[string]map[string]rawCommandSpec
	if len(raw) == 0 || len(raw) > 64<<10 || json.Unmarshal(raw, &in) != nil {
		return nil
	}
	out := map[string]map[string]rawCommandSpec{}
	for mod, cmds := range in {
		for cmd, sp := range cmds {
			if !slices.Contains(remote[mod], cmd) || len(sp.Params) > 50 || len(sp.Result) > 100 {
				continue
			}
			sp.Label, sp.Desc = trunc(sp.Label, 64), trunc(sp.Desc, 300)
			if !iconRe.MatchString(sp.Icon) {
				sp.Icon = ""
			}
			if sp.Timeout < 1 || sp.Timeout > maxCommandTimeout {
				sp.Timeout = 0
			}
			var params []dto.CommandParam
			for _, p := range sp.Params {
				if p.Key == "" || len(p.Key) > 64 || !slices.Contains(paramTypes, p.Type) {
					continue
				}
				params = append(params, p)
			}
			sp.Params = params
			if out[mod] == nil {
				out[mod] = map[string]rawCommandSpec{}
			}
			out[mod][cmd] = sp
		}
	}
	if len(out) == 0 {
		return nil
	}
	b, _ := json.Marshal(out)
	return b
}

// DeviceCommands 设备开放给开放 API 的命令（不含系统内部命令），附说明。
func DeviceCommands(d *model.Device) []dto.CommandSpec {
	var schema map[string]map[string]rawCommandSpec
	if len(d.CommandSchema) > 0 {
		_ = json.Unmarshal(d.CommandSchema, &schema)
	}
	var out []dto.CommandSpec
	for mod, cmds := range dto.DeviceFrom(d).RemoteCommands {
		for _, cmd := range cmds {
			if internalCommands[mod+"/"+cmd] {
				continue
			}
			sp := dto.CommandSpec{Module: mod, Command: cmd, Label: cmd, Params: []dto.CommandParam{}, Result: []dto.CommandField{}}
			if r, ok := schema[mod][cmd]; ok {
				sp.Declared = true
				if r.Label != "" {
					sp.Label = r.Label
				}
				sp.Desc = r.Desc
				sp.Icon = r.Icon
				sp.Timeout = r.Timeout
				if r.Params != nil {
					sp.Params = r.Params
				}
				if r.Result != nil {
					sp.Result = r.Result
				}
			}
			out = append(out, sp)
		}
	}
	sortSpecs(out)
	return out
}

// CatalogWindow 命令目录只统计这段时间内连过服务器的设备：长期不上线的（报废、卸载）自动退出目录，
// 不让它们的命令一直留在开放 API 里；设备记录本身保留，重新上线后自动回到目录。
const CatalogWindow = 30 * 24 * time.Hour

// CommandCatalog 近期在线设备的命令合集：同一命令以版本最高的设备的说明为准，并列出支持它的设备；
// Online 表示当前是否有支持它的设备在线。
func CommandCatalog(devices []model.Device) []dto.CommandSpec {
	cutoff := time.Now().Add(-CatalogWindow)
	type entry struct {
		spec    dto.CommandSpec
		version string
	}
	all := map[string]*entry{}
	for i := range devices {
		d := &devices[i]
		if !d.Online && d.LastSeenAt.Before(cutoff) {
			continue
		}
		for _, sp := range DeviceCommands(d) {
			k := sp.Module + "/" + sp.Command
			e := all[k]
			if e == nil {
				e = &entry{spec: sp, version: d.Version}
				all[k] = e
			} else if (sp.Declared && !e.spec.Declared) || (sp.Declared == e.spec.Declared && release.CompareVersions(d.Version, e.version) > 0) {
				devs, online := e.spec.Devices, e.spec.Online
				e.spec, e.version = sp, d.Version
				e.spec.Devices, e.spec.Online = devs, online
			}
			e.spec.Devices = append(e.spec.Devices, d.ID)
			e.spec.Online = e.spec.Online || d.Online
		}
	}
	out := make([]dto.CommandSpec, 0, len(all))
	for _, e := range all {
		sort.Slice(e.spec.Devices, func(i, j int) bool { return e.spec.Devices[i] < e.spec.Devices[j] })
		out = append(out, e.spec)
	}
	sortSpecs(out)
	return out
}

func sortSpecs(list []dto.CommandSpec) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].Module != list[j].Module {
			return list[i].Module < list[j].Module
		}
		return list[i].Command < list[j].Command
	})
}

// maxCommandTimeout 模块可声明的命令执行上限（秒）。
const maxCommandTimeout = 3600

// CommandTimeout 命令下发后等待结果的上限：模块在 +commandSchema 里声明的 timeout，默认 sentTimeout。
func CommandTimeout(d *model.Device, module, command string) time.Duration {
	if d == nil || len(d.CommandSchema) == 0 {
		return sentTimeout
	}
	var schema map[string]map[string]struct {
		Timeout int `json:"timeout"`
	}
	if json.Unmarshal(d.CommandSchema, &schema) != nil {
		return sentTimeout
	}
	if t := schema[module][command].Timeout; t >= 1 && t <= maxCommandTimeout {
		return time.Duration(t) * time.Second
	}
	return sentTimeout
}

// PublicDevice 开放 API 返回的设备：去掉系统内部命令（调用它们只会 403）。
func PublicDevice(d dto.Device) dto.Device {
	cmds := make(map[string][]string, len(d.RemoteCommands))
	for mod, list := range d.RemoteCommands {
		var keep []string
		for _, c := range list {
			if !internalCommands[mod+"/"+c] {
				keep = append(keep, c)
			}
		}
		if len(keep) > 0 {
			cmds[mod] = keep
		}
	}
	d.RemoteCommands = cmds
	return d
}

// FindCommand 设备上的某个命令；不存在或未开放返回 nil。
func FindCommand(d *model.Device, module, command string) *dto.CommandSpec {
	for _, sp := range DeviceCommands(d) {
		if sp.Module == module && sp.Command == command {
			return &sp
		}
	}
	return nil
}

// ValidateArgs 按命令说明校验参数：必填、类型、范围，不认识的参数拒绝（防止拼写错误被静默忽略）。
// 设备没有提供说明的命令不校验，原样下发（设备端仍会校验）。
func ValidateArgs(sp *dto.CommandSpec, args map[string]any) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	if !sp.Declared {
		return args, nil
	}
	out := map[string]any{}
	known := map[string]dto.CommandParam{}
	for _, p := range sp.Params {
		known[p.Key] = p
	}
	for k := range args {
		if _, ok := known[k]; !ok {
			return nil, errcode.InvalidParams.WithMessage("未知参数 %s", k)
		}
	}
	for _, p := range sp.Params {
		v, ok := args[p.Key]
		if !ok || v == nil {
			if p.Required {
				return nil, errcode.InvalidParams.WithMessage("缺少必填参数 %s", p.Key)
			}
			continue
		}
		nv, err := normalizeParam(p, v)
		if err != nil {
			return nil, errcode.InvalidParams.WithMessage("参数 %s：%s", p.Key, err.Error())
		}
		out[p.Key] = nv
	}
	return out, nil
}

func normalizeParam(p dto.CommandParam, v any) (any, error) {
	switch p.Type {
	case "object":
		if m, ok := v.(map[string]any); ok {
			return m, nil
		}
		return nil, errors.New("应为对象")
	case "array":
		if a, ok := v.([]any); ok {
			return a, nil
		}
		return nil, errors.New("应为数组")
	}
	nv, err := normalizeValue(p.ConfigItem, v)
	if err != nil {
		return nil, err
	}
	if f, ok := nv.(float64); ok && p.Type == "int" {
		return int64(f), nil
	}
	return nv, nil
}
