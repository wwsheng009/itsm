package protocol

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// SSEFrame 一个组装完成的 Server-Sent Events 帧。
// 语义对齐参考实现 adapter/sse.go，收敛为最小必要实现（itsm 侧暂无 id/retry 消费方）。
type SSEFrame struct {
	Event string
	Data  string
}

// scanSSEFrames 解析 SSE 流：兼容 LF/CRLF、data 多行以 \n 连接、
// 忽略注释行（":" 前缀），EOF 时派发未终结帧。
func scanSSEFrames(reader io.Reader, handle func(SSEFrame) (bool, error)) error {
	if reader == nil {
		return fmt.Errorf("sse reader is required")
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 20*1024*1024)

	frame := SSEFrame{}
	dataLines := make([]string, 0, 1)
	hasFields := false

	dispatch := func() (bool, error) {
		if !hasFields {
			return true, nil
		}
		frame.Data = strings.Join(dataLines, "\n")
		keepGoing, err := handle(frame)
		frame = SSEFrame{}
		dataLines = dataLines[:0]
		hasFields = false
		return keepGoing, err
	}

	firstLine := true
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if firstLine {
			line = strings.TrimPrefix(line, "\uFEFF")
			firstLine = false
		}
		if line == "" {
			keepGoing, err := dispatch()
			if err != nil || !keepGoing {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value, found := strings.Cut(line, ":")
		if !found {
			value = ""
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			frame.Event = strings.TrimSpace(value)
			hasFields = true
		case "data":
			dataLines = append(dataLines, value)
			hasFields = true
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read sse stream: %w", err)
	}
	_, err := dispatch()
	return err
}
