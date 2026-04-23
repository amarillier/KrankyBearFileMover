package main

import (
	"fmt"
	"os"
	"strings"
)

func fileAgentDebug() bool {
	v := strings.TrimSpace(os.Getenv("FILEMOVER_FILEAGENT_DEBUG"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func fileAgentDebugf(format string, args ...interface{}) {
	if !fileAgentDebug() {
		return
	}
	fmt.Fprintf(os.Stderr, "fileagent debug: "+format+"\n", args...)
}
