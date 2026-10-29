package main

import (
	"bufio"
	"regexp"
	"sort"
	"strings"
)

// appErrKind: one kind of application error from app.log — the message after "error at <path>:" (echo's error
// handler) or the JSON "message"/"msg" field, with numbers masked so the same error groups together.
type appErrKind struct {
	endpoint string // route from "error at /api/x:" when present ("" otherwise)
	message  string
	count    int
}

var (
	jsonMsgRe   = regexp.MustCompile(`"(?:message|msg|error)"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	errorAtRe   = regexp.MustCompile(`error at (\S+):\s+(.*)$`)
	echoCodeRe  = regexp.MustCompile(`^code=\d+, message=`)
	maskDigitRe = regexp.MustCompile(`\d+`)
)

// appErrorKinds reads app.log (journald text of the app unit) and groups error lines by message.
func appErrorKinds(path string) (kinds []appErrKind, total int) {
	f, err := openMaybeGz(path)
	if err != nil {
		return nil, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	by := map[string]*appErrKind{}
	for sc.Scan() {
		line := sc.Text()
		if !appErrRe.MatchString(line) {
			continue
		}
		msg := line
		if m := jsonMsgRe.FindStringSubmatch(line); m != nil {
			msg = strings.ReplaceAll(m[1], `\"`, `"`)
		} else if i := strings.Index(line, "]: "); i >= 0 { // journald: "host unit[pid]: text"
			msg = line[i+3:]
		}
		ep := ""
		if m := errorAtRe.FindStringSubmatch(msg); m != nil {
			ep, msg = m[1], m[2]
		}
		msg = echoCodeRe.ReplaceAllString(msg, "")
		msg = maskDigitRe.ReplaceAllString(strings.TrimSpace(msg), "N")
		if len(msg) > 240 {
			msg = msg[:240] + "…"
		}
		total++
		k := ep + "\x00" + msg
		if by[k] == nil {
			by[k] = &appErrKind{endpoint: ep, message: msg}
		}
		by[k].count++
	}
	for _, k := range by {
		kinds = append(kinds, *k)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i].count > kinds[j].count })
	return kinds, total
}
