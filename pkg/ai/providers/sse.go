package providers

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

type serverSentEvent struct {
	Event string
	Data  string
	Raw   []string
}

func iterateSSE(reader io.Reader, handle func(serverSentEvent) error) error {
	buffered := bufio.NewReaderSize(reader, 64*1024)
	eventType := ""
	data := []string{}
	raw := []string{}
	flush := func() error {
		if eventType == "" && len(data) == 0 {
			return nil
		}
		event := serverSentEvent{Event: eventType, Data: strings.Join(data, "\n"), Raw: append([]string{}, raw...)}
		eventType = ""
		data = nil
		raw = nil
		return handle(event)
	}

	processLine := func(line string) error {
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			return nil
		}
		raw = append(raw, line)
		if strings.HasPrefix(line, ":") {
			return nil
		}
		field := line
		value := ""
		if index := strings.Index(line, ":"); index >= 0 {
			field = line[:index]
			value = line[index+1:]
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "event":
			eventType = value
		case "data":
			data = append(data, value)
		}
		return nil
	}

	for {
		line, err := buffered.ReadString('\n')
		if len(line) > 0 {
			if processErr := processLine(line); processErr != nil {
				return processErr
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	return flush()
}
