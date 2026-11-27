package pine

import (
	"strings"
	"testing"
)

const arrayScript = `//@version=6
indicator("x")
var array<float> last3 = array.new<float>()
last3.push(close)
if last3.size() > 3
    last3.shift()
float total = 0
for v in last3
    total += v
float w = 0
for [i, v] in last3
    w += i * v
int[] tmp = array.from(3, 1, 2)
array.sort(tmp, order.descending)
fresh = array.new_float(2, close)
plot(total, "sum3")
plot(array.indexof(last3, close), "pos")
plot(tmp.get(0) * 10 + tmp.last(), "sorted")
plot(fresh.sum(), "fresh")
plot(w, "weighted")
`

// Arrays keep what a var holds from bar to bar, and their functions,
// methods and loops see it.
func TestArrays(t *testing.T) {
	_, m := run(t, arrayScript, closes(1, 2, 3, 4, 5))
	checkPlot(t, m, 0, 1, 3, 6, 9, 12)
	checkPlot(t, m, 1, 0, 1, 2, 2, 2)
	checkPlot(t, m, 2, 31, 31, 31, 31, 31)
	checkPlot(t, m, 3, 2, 4, 6, 8, 10)
	checkPlot(t, m, 4, 0, 2, 8, 11, 14)
	if n := len(m.arrs.byID); n != 1 { // last3: the others were dropped as their bars ended
		t.Fatalf("%d arrays kept", n)
	}
}

// The newest bar, run again as it changes, starts from the arrays as they
// were before it: its pushes are not counted twice.
func TestArraysRunAgain(t *testing.T) {
	checkIncremental(t, "arrays", arrayScript)
}

// A call that gives nothing, last in an if, a switch or a function, runs:
// it was dropped as the block's value.
func TestVoidCallEndingABlock(t *testing.T) {
	_, m := run(t, `//@version=6
indicator("x")
var a = array.new_float()
add(v) =>
    a.push(v)
if close > 2
    a.push(close)
switch
    close > 3 => a.push(close)
add(0)
plot(a.size())
`, closes(1, 2, 3, 4))
	checkPlot(t, m, 0, 1, 2, 4, 7)
}

func TestArrayIndexOutOfRange(t *testing.T) {
	s, err := Compile("indicator(\"x\")\na = array.new_float(2)\nplot(a.get(5))")
	if err != nil {
		t.Fatal(err)
	}
	if m := s.Run(nil, closes(1), nil, 0, Env{}); m.Err == nil || !strings.Contains(m.Err.Error(), "超出范围") {
		t.Fatalf("error %v", m.Err)
	}
}
