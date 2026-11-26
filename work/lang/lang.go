// Package lang says OpenBar's words in the language it shows. The code
// writes them in Chinese, as OpenBar first spoke; en has them in English.
package lang

import (
	"fmt"
	"strings"
	"sync/atomic"
)

var english atomic.Bool

// Set shows the language of tag, as mygo.App.Locale names it: Chinese for
// any of Chinese ("zh-Hans", "zh-Hant"), English for the rest.
func Set(tag string) { english.Store(tag != "" && !strings.HasPrefix(tag, "zh")) }

// English is whether OpenBar shows English.
func English() bool { return english.Load() }

// T is s, written in Chinese, in the language shown: s itself where en has
// no English for it, as for a stock's own Chinese name.
func T(s string) string {
	if english.Load() {
		if e, ok := en[s]; ok {
			return e
		}
	}
	return s
}

// F is fmt.Sprintf of format, written in Chinese, in the language shown.
func F(format string, args ...any) string { return fmt.Sprintf(T(format), args...) }
