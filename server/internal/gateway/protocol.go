// Package gateway 设备 WebSocket 网关，协议与手机端 net.link 模块一一对应。
//
// 每帧一个 JSON：{"id", "type", "payload", "ts"}。
//
//	设备 → 服务端：hello、ping、pong、result、自定义事件
//	服务端 → 设备：ack、pong、ping、invoke
package gateway

import "encoding/json"

const (
	TypeHello  = "hello"
	TypePing   = "ping"
	TypePong   = "pong"
	TypeAck    = "ack"
	TypeInvoke = "invoke"
	TypeResult = "result"
)

// Message 线上的一条消息。
type Message struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Level   string          `json:"level,omitempty"` // 仅事件
	Payload json.RawMessage `json:"payload,omitempty"`
	TS      float64         `json:"ts,omitempty"`
	Error   *ResultError    `json:"error,omitempty"` // 仅 result
}

type ResultError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type helloPayload struct {
	Device         string              `json:"device"`
	Name           string              `json:"name"`
	Version        string              `json:"version"`
	Network        string              `json:"network"`
	RemoteCommands map[string][]string `json:"remote_commands"`
	ConfigVersion  int64               `json:"config_version"`
	ConfigSchema   json.RawMessage     `json:"config_schema"`
	CommandSchema  json.RawMessage     `json:"command_schema"`
	Info           json.RawMessage     `json:"info"`
}

type invokePayload struct {
	Module    string          `json:"module"`
	Command   string          `json:"command"`
	Arguments json.RawMessage `json:"arguments"`
}
