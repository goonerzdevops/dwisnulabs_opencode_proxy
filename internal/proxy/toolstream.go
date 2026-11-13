package proxy

import "strings"

// toolMarkupFilter incrementally scans streamed assistant text. Everything before a
// textual tool-call marker is released immediately (minus a small tail that might be the
// beginning of a marker split across chunks); everything from the marker onward is
// buffered so it can be converted into structured tool_calls on Finish.
type toolMarkupFilter struct {
	schemas     toolSchemas
	accText     string
	sent        int
	inMarkup    bool
	markupStart int
}

func newToolMarkupFilter(schemas toolSchemas) *toolMarkupFilter {
	return &toolMarkupFilter{schemas: schemas}
}

// InMarkup reports whether a tool-call marker has been detected.
func (f *toolMarkupFilter) InMarkup() bool { return f.inMarkup }

// Push feeds a new text fragment and returns the text that is safe to emit now.
func (f *toolMarkupFilter) Push(text string) string {
	f.accText += text
	if f.inMarkup {
		return ""
	}

	pending := f.accText[f.sent:]
	if loc := toolMarkupStartRe.FindStringIndex(pending); loc != nil {
		f.inMarkup = true
		f.markupStart = f.sent + loc[0]
		out := pending[:loc[0]]
		f.sent = f.markupStart
		return out
	}

	// Hold back a trailing fragment that may still become a marker.
	cut := len(pending)
	if i := strings.LastIndex(pending, "<"); i >= 0 && couldBeMarkerPrefix(pending[i:]) {
		cut = i
	}
	out := pending[:cut]
	f.sent += cut
	return out
}

// Finish flushes the filter. It returns any remaining plain text and the parsed tool calls.
// When a marker was seen but nothing parseable was found the raw marker text is returned
// as plain text so the user never receives an empty answer.
func (f *toolMarkupFilter) Finish() (string, []ToolCall) {
	if !f.inMarkup {
		out := f.accText[f.sent:]
		f.sent = len(f.accText)
		return out, nil
	}
	section := f.accText[f.markupStart:]
	f.sent = len(f.accText)
	_, calls := ParseToolCalls(section, f.schemas)
	if len(calls) > 0 {
		return "", calls
	}
	return section, nil
}

func trimMarkupSeparators(s string) string {
	return strings.TrimLeft(s, " \t\r\n|｜")
}

// couldBeMarkerPrefix reports whether tail (which starts with '<') might still grow into a
// tool-call start marker once more characters arrive.
func couldBeMarkerPrefix(tail string) bool {
	s := strings.ToLower(strings.TrimPrefix(tail, "<"))
	s = trimMarkupSeparators(s)
	if s == "" {
		return true
	}
	if strings.HasPrefix("dsml", s) {
		return true
	}
	hadDSML := false
	if strings.HasPrefix(s, "dsml") {
		hadDSML = true
		s = trimMarkupSeparators(s[len("dsml"):])
		if s == "" {
			return true
		}
	}

	keywords := []string{"tool_calls", "tool_call", "function_calls"}
	if hadDSML {
		keywords = append(keywords, "calls")
	}
	for _, kw := range keywords {
		if strings.HasPrefix(kw, s) {
			return true
		}
		// keyword fully typed, waiting for optional whitespace and '>'
		if strings.HasPrefix(s, kw) && strings.TrimSpace(s[len(kw):]) == "" {
			return true
		}
		// "tool_call" + "s" is covered by the "tool_calls" keyword prefix check above.
	}

	// invoke name=
	if strings.HasPrefix("invoke", s) {
		return true
	}
	if strings.HasPrefix(s, "invoke") {
		rest := s[len("invoke"):]
		if strings.TrimSpace(rest) == "" {
			return true
		}
		t := strings.TrimLeft(rest, " \t\r\n")
		if t == rest {
			return false // "invokeX" is not a marker
		}
		if strings.HasPrefix("name", t) {
			return true
		}
		if strings.HasPrefix(t, "name") && strings.TrimSpace(t[len("name"):]) == "" {
			return true
		}
	}
	return false
}
