package agent

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

type frameType string

const (
	frameTypeExecRequest    frameType = "exec_request"
	frameTypeStdout         frameType = "stdout"
	frameTypeStderr         frameType = "stderr"
	frameTypeResult         frameType = "result"
	frameTypeError          frameType = "error"
	frameTypeStdinChunk     frameType = "stdin_chunk"
	frameTypeStdinClose     frameType = "stdin_close"
	frameTypePing           frameType = "ping"
	frameTypePong           frameType = "pong"
	frameTypeFilePutRequest frameType = "file_put_request"
	frameTypeFilePutChunk   frameType = "file_put_chunk"
	frameTypeFilePutClose   frameType = "file_put_close"
	frameTypeFilePutResult  frameType = "file_put_result"
	frameTypeFileGetRequest frameType = "file_get_request"
	frameTypeFileGetChunk   frameType = "file_get_chunk"
	frameTypeFileGetResult  frameType = "file_get_result"
)

type rawFrame struct {
	Type    frameType       `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type execRequestPayload struct {
	Path         string            `json:"path"`
	Args         []string          `json:"args"`
	Env          map[string]string `json:"env,omitempty"`
	WorkingDir   string            `json:"working_dir,omitempty"`
	TimeoutMilli int64             `json:"timeout_ms,omitempty"`
	Stream       bool              `json:"stream"`
	User         string            `json:"user,omitempty"`
}

type execResultPayload struct {
	ExitCode      int       `json:"exit_code"`
	Stdout        []byte    `json:"stdout,omitempty"`
	Stderr        []byte    `json:"stderr,omitempty"`
	DurationMilli int64     `json:"duration_ms"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
	ErrorMessage  string    `json:"error,omitempty"`
}

type chunkPayload struct {
	Data []byte `json:"data"`
}

type stdinPayload struct {
	Data []byte `json:"data"`
}

type filePutRequestPayload struct {
	Path string `json:"path"`
	Mode uint32 `json:"mode,omitempty"`
}

type fileGetRequestPayload struct {
	Path string `json:"path"`
}

type fileTransferResultPayload struct {
	Bytes int64  `json:"bytes"`
	Error string `json:"error,omitempty"`
}

type errorPayload struct {
	Message string `json:"message"`
}

type pongPayload struct {
	Timestamp time.Time `json:"timestamp"`
}

type frameWriter struct {
	enc *json.Encoder
	mu  sync.Mutex
}

func newFrameWriter(w io.Writer) *frameWriter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &frameWriter{enc: enc}
}

func (w *frameWriter) send(typ frameType, payload any) error {
	frame := rawFrame{Type: typ}
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		frame.Payload = data
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	return w.enc.Encode(&frame)
}

func readFrame(dec *json.Decoder) (*rawFrame, error) {
	var frame rawFrame
	if err := dec.Decode(&frame); err != nil {
		return nil, err
	}
	return &frame, nil
}
