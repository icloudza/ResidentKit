package open

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/icloudza/ResidentKit/server/internal/dto"
)

// BuildSpec 在基础规范（openapi.json）上为目录中的每个命令生成独立接口：
// POST /devices/{id}/commands/{模块}/{命令}，请求体是命令参数，返回字段来自设备上报的说明。
func BuildSpec(catalog []dto.CommandSpec) []byte {
	var doc map[string]any
	if json.Unmarshal(spec, &doc) != nil {
		return spec
	}
	paths, _ := doc["paths"].(map[string]any)
	for _, c := range catalog {
		paths[fmt.Sprintf("/devices/{id}/commands/%s/%s", c.Module, c.Command)] = map[string]any{"post": commandOp(c)}
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return spec
	}
	return b
}

func commandOp(c dto.CommandSpec) map[string]any {
	desc := c.Desc
	if !c.Declared {
		desc = "设备未提供该命令的说明，参数原样下发。"
	}
	desc += "\n\n需要 commands:invoke。默认同步：等待设备返回（最多 timeout 秒），结果在 data.result。"
	if c.Timeout > 0 {
		desc += fmt.Sprintf("\n\n设备端执行上限 %d 秒，超过仍无结果记为结果未知（lost）。", c.Timeout)
	}
	op := map[string]any{
		"tags":        []string{"命令"},
		"summary":     fmt.Sprintf("%s（%s · %s）", c.Label, c.Module, c.Command),
		"description": desc,
		"operationId": opID(c),
		"parameters": []any{
			map[string]any{"name": "id", "in": "path", "required": true, "description": "设备 ID", "schema": map[string]any{"type": "integer", "format": "int64"}},
			ref("parameters", "async"), ref("parameters", "timeout"), ref("parameters", "ttl"), ref("parameters", "idem"),
		},
		"responses": map[string]any{
			"200": jsonResp("成功：设备返回的结果", map[string]any{"type": "object", "properties": map[string]any{
				"invocation_id": map[string]any{"type": "integer", "format": "int64"},
				"duration_ms":   map[string]any{"type": "integer", "description": "下发到返回的耗时"},
				"result":        resultSchema(c),
			}}),
			"202": jsonResp("已受理，之后查询结果：异步调用（async=true），或带 ttl 时设备离线、指令排队中", ref("schemas", "Pending")),
			"400": errResp("参数错误（10001）：缺少必填参数、类型不符或有未知参数"),
			"404": errResp("设备不存在、不在密钥范围内，或设备没有开放该命令（10002）"),
			"409": errResp("设备离线（30001），或幂等键已用于另一条指令（10004：设备、命令或参数不同）"),
			"422": errResp("设备执行失败（30005），msg 为设备返回的原因，data.device_error_code 为设备错误码"),
			"502": errResp("结果未知（30006）：下发后连接断开或设备超时未返回"),
			"504": errResp("设备未在 timeout 秒内返回（30003），data.invocation_id 可用于查询"),
		},
	}
	if len(c.Params) > 0 || !c.Declared {
		op["requestBody"] = map[string]any{"required": hasRequired(c), "content": map[string]any{
			"application/json": map[string]any{"schema": paramsSchema(c), "example": example(c)}}}
	}
	return op
}

func paramsSchema(c dto.CommandSpec) map[string]any {
	if !c.Declared {
		return map[string]any{"type": "object", "description": "参数由设备模块定义"}
	}
	props := map[string]any{}
	var required []string
	for _, p := range c.Params {
		s := typeSchema(p.Type)
		if p.Label != "" || p.Desc != "" {
			s["description"] = strings.TrimSpace(p.Label + " " + p.Desc)
		}
		if p.Min != nil {
			s["minimum"] = *p.Min
		}
		if p.Max != nil {
			s["maximum"] = *p.Max
		}
		if len(p.Options) > 0 {
			s["enum"] = p.Options
		}
		if p.Default != nil {
			s["default"] = p.Default
		}
		props[p.Key] = s
		if p.Required {
			required = append(required, p.Key)
		}
	}
	out := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func resultSchema(c dto.CommandSpec) map[string]any {
	if len(c.Result) == 0 {
		return map[string]any{"type": "object"}
	}
	props := map[string]any{}
	for _, f := range c.Result {
		s := typeSchema(f.Type)
		if f.Desc != "" {
			s["description"] = f.Desc
		}
		props[f.Key] = s
	}
	return map[string]any{"type": "object", "properties": props}
}

func typeSchema(t string) map[string]any {
	switch t {
	case "bool":
		return map[string]any{"type": "boolean"}
	case "int":
		return map[string]any{"type": "integer"}
	case "number":
		return map[string]any{"type": "number"}
	case "object":
		return map[string]any{"type": "object"}
	case "array":
		return map[string]any{"type": "array", "items": map[string]any{}}
	}
	return map[string]any{"type": "string"}
}

func example(c dto.CommandSpec) map[string]any {
	ex := map[string]any{}
	for _, p := range c.Params {
		switch {
		case p.Default != nil:
			ex[p.Key] = p.Default
		case p.Type == "bool":
			ex[p.Key] = true
		case p.Type == "int" || p.Type == "number":
			if p.Min != nil {
				ex[p.Key] = *p.Min
			} else {
				ex[p.Key] = 1
			}
		case p.Type == "enum" && len(p.Options) > 0:
			ex[p.Key] = p.Options[0]
		case p.Type == "object":
			ex[p.Key] = map[string]any{}
		case p.Type == "array":
			ex[p.Key] = []any{}
		default:
			ex[p.Key] = ""
		}
	}
	return ex
}

func hasRequired(c dto.CommandSpec) bool {
	for _, p := range c.Params {
		if p.Required {
			return true
		}
	}
	return false
}

func opID(c dto.CommandSpec) string {
	r := strings.NewReplacer(".", "_", "-", "_")
	return "cmd_" + r.Replace(c.Module) + "_" + r.Replace(c.Command)
}

func ref(kind, name string) map[string]any {
	return map[string]any{"$ref": "#/components/" + kind + "/" + name}
}

func jsonResp(desc string, data any) map[string]any {
	return map[string]any{"description": desc, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{
		"type": "object", "properties": map[string]any{
			"code": map[string]any{"type": "integer", "example": 0}, "msg": map[string]any{"type": "string", "example": "ok"}, "data": data}}}}}
}

func errResp(desc string) map[string]any {
	return map[string]any{"description": desc, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{
		"type": "object", "properties": map[string]any{
			"code": map[string]any{"type": "integer"}, "msg": map[string]any{"type": "string"}, "data": ref("schemas", "Pending")}}}}}
}
