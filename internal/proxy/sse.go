package proxy

import (
	"bufio"
	"io"
	"strings"
)

// SSEEvent is one complete server-sent event frame.
type SSEEvent struct {
	Event string
	Data  []byte
}

// ScanSSE reads SSE frames from src, invoking handle for every complete frame.
// A `[DONE]` sentinel and a `stop` return from handle both end the scan early
// without an error, matching the OpenAI streaming contract.
func ScanSSE(src io.Reader, handle func(SSEEvent) (bool, error)) error {
	reader := bufio.NewReaderSize(src, 128*1024)
	var (
		eventName string
		dataLines []string
	)

	flush := func() (bool, error) {
		if len(dataLines) == 0 {
			eventName = ""
			return false, nil
		}
		payload := strings.Join(dataLines, "\n")
		event := SSEEvent{Event: eventName, Data: []byte(payload)}
		eventName = ""
		dataLines = nil
		if strings.TrimSpace(payload) == "[DONE]" {
			return true, nil
		}
		return handle(event)
	}

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				stop, handleErr := flush()
				if handleErr != nil {
					return handleErr
				}
				if stop {
					return nil
				}
			case strings.HasPrefix(trimmed, ":"):
				// Keep-alive comment.
			case strings.HasPrefix(trimmed, "event:"):
				eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			case strings.HasPrefix(trimmed, "data:"):
				dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(trimmed, "data:"), " "))
			}
		}
		if err != nil {
			if err == io.EOF {
				_, handleErr := flush()
				return handleErr
			}
			return err
		}
	}
}
