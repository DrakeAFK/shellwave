package ws

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	ClientTypeConnect = "connect"
	ClientTypeInput   = "input"
	ClientTypeResize  = "resize"
	ClientTypePing    = "ping"

	ServerTypeStatus = "status"
	ServerTypeOutput = "output"
	ServerTypeError  = "error"
	ServerTypeExit   = "exit"

	StateConnecting   = "connecting"
	StateConnected    = "connected"
	StateError        = "error"
	StateDisconnected = "disconnected"
	StateIdle         = "idle"
)

type Auth struct {
	Type       string `json:"type,omitempty"`
	Password   string `json:"password,omitempty"`
	KeyPath    string `json:"keyPath,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
	UseAgent   bool   `json:"useAgent,omitempty"`
}

type ClientMessage struct {
	Type     string `json:"type"`
	DeviceID string `json:"deviceId,omitempty"`
	AuthRef  string `json:"authRef,omitempty"`
	Host     string `json:"host,omitempty"`
	User     string `json:"user,omitempty"`
	Port     int    `json:"port,omitempty"`
	Auth     Auth   `json:"auth,omitempty"`
	Data     string `json:"data,omitempty"`
	Cols     int    `json:"cols,omitempty"`
	Rows     int    `json:"rows,omitempty"`
}

type ServerMessage struct {
	Type      string          `json:"type"`
	State     string          `json:"state,omitempty"`
	Data      string          `json:"data,omitempty"`
	Message   string          `json:"message,omitempty"`
	ErrorCode string          `json:"errorCode,omitempty"`
	HostKey   *HostKeyDetails `json:"hostKey,omitempty"`
	Code      *int            `json:"code,omitempty"`
}

type HostKeyDetails struct {
	Host                    string `json:"host"`
	Port                    int    `json:"port"`
	KeyType                 string `json:"keyType"`
	FingerprintSHA256       string `json:"fingerprintSha256"`
	PublicKey               string `json:"publicKey"`
	KnownFingerprintSHA256  string `json:"knownFingerprintSha256,omitempty"`
	KnownPublicKeyAvailable bool   `json:"knownPublicKeyAvailable,omitempty"`
}

func DecodeClientMessage(data []byte) (ClientMessage, error) {
	var msg ClientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return msg, err
	}
	if msg.Type == "" {
		return msg, errors.New("message type is required")
	}
	return msg, nil
}

func EncodeServerMessage(msg ServerMessage) ([]byte, error) {
	if msg.Type == "" {
		return nil, errors.New("message type is required")
	}
	return json.Marshal(msg)
}

func ValidateConnect(msg ClientMessage) error {
	if msg.Type != ClientTypeConnect {
		return fmt.Errorf("first message must be %q", ClientTypeConnect)
	}
	if msg.Host == "" {
		return errors.New("host is required")
	}
	if msg.User == "" {
		return errors.New("user is required")
	}
	if msg.Port < 0 || msg.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}

func Status(state string) ServerMessage {
	return ServerMessage{Type: ServerTypeStatus, State: state}
}

func Output(data string) ServerMessage {
	return ServerMessage{Type: ServerTypeOutput, Data: data}
}

func Error(message string) ServerMessage {
	return ServerMessage{Type: ServerTypeError, Message: message}
}

func ErrorWithCode(code, message string) ServerMessage {
	return ServerMessage{Type: ServerTypeError, ErrorCode: code, Message: message}
}

func HostKeyError(code, message string, hostKey HostKeyDetails) ServerMessage {
	return ServerMessage{Type: ServerTypeError, ErrorCode: code, Message: message, HostKey: &hostKey}
}

func Exit(code int) ServerMessage {
	return ServerMessage{Type: ServerTypeExit, Code: &code}
}
