package utils

import "strings"

func UcaseFirst(s string) string {
	retStr := ""
	for _, chr := range strings.Split(s, "") {
		if retStr == "" {
			retStr += strings.ToUpper(chr)
		} else {
			retStr += strings.ToLower(chr)
		}
	}
	return retStr
}
