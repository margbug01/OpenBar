package pine

import (
	"math"
	"slices"
	"strings"

	"candle/lang"
)

// arrays are a machine's arrays by id. A variable holding an array holds
// its id; ids are never used again, so an id kept from a bar whose arrays
// are gone finds nothing.
type arrays struct {
	byID map[int][]float64
	next int
}

func (a arrays) clone() arrays {
	out := arrays{byID: make(map[int][]float64, len(a.byID)), next: a.next}
	for id, v := range a.byID {
		out.byID[id] = slices.Clone(v)
	}
	return out
}

// sweep drops, as a bar ends, the arrays no var holds.
func (a arrays) sweep(m *Machine, slots []int) {
	if len(a.byID) == 0 {
		return
	}
	keep := make(map[int]bool, len(slots))
	for _, s := range slots {
		if v := m.buf[s][m.i]; !math.IsNaN(v) {
			keep[int(v)] = true
		}
	}
	for id := range a.byID {
		if !keep[id] {
			delete(a.byID, id)
		}
	}
}

// maxArray is Pine's limit on an array's size.
const maxArray = 100_000

func (m *Machine) runErr(format string, args ...any) {
	panic(&RunError{Bar: m.i, Msg: lang.F(format, args...)})
}

func (m *Machine) newArray(v []float64) float64 {
	if len(v) > maxArray {
		m.runErr("数组最多 %d 个元素", maxArray)
	}
	m.arrs.next++
	m.arrs.byID[m.arrs.next] = v
	return float64(m.arrs.next)
}

func (m *Machine) array(id float64) []float64 {
	v, ok := m.arrs.byID[int(id)]
	if math.IsNaN(id) || !ok {
		m.runErr("数组是 na")
	}
	return v
}

// index is i of an array of n elements, from the end when negative.
func (m *Machine) index(i float64, n int, upto int) int {
	k := int(i)
	if k < 0 {
		k += n
	}
	if math.IsNaN(i) || k < 0 || k >= upto {
		m.runErr("数组下标 %v 超出范围（数组有 %d 个元素）", i, n)
	}
	return k
}

// arrOp is a function on an array: what it is given after the array, and
// what it does with the array and those values.
type arrOp struct {
	params []string
	gives  vkind // kFloat a value, kVoid nothing
	newArr bool  // the value is an array's id
	run    func(m *Machine, id float64, a []float64, v []float64) float64
}

func store(m *Machine, id float64, a []float64) {
	if len(a) > maxArray {
		m.runErr("数组最多 %d 个元素", maxArray)
	}
	m.arrs.byID[int(id)] = a
}

func numbers(a []float64) []float64 {
	out := make([]float64, 0, len(a))
	for _, v := range a {
		if !math.IsNaN(v) {
			out = append(out, v)
		}
	}
	return out
}

func sorted(a []float64) []float64 {
	s := numbers(a)
	slices.Sort(s)
	return s
}

func variance(a []float64, biased float64) float64 {
	s := numbers(a)
	n := float64(len(s))
	if n == 0 || biased == 0 && n < 2 {
		return nan
	}
	mean := 0.0
	for _, v := range s {
		mean += v
	}
	mean /= n
	sq := 0.0
	for _, v := range s {
		sq += (v - mean) * (v - mean)
	}
	if biased == 0 {
		return sq / (n - 1)
	}
	return sq / n
}

func eqNa(a, b float64) bool { return a == b || math.IsNaN(a) && math.IsNaN(b) }

var arrOps = map[string]arrOp{
	"size": {gives: kFloat, run: func(_ *Machine, _ float64, a, _ []float64) float64 { return float64(len(a)) }},
	"get": {params: []string{"index"}, gives: kFloat, run: func(m *Machine, _ float64, a, v []float64) float64 {
		return a[m.index(v[0], len(a), len(a))]
	}},
	"set": {params: []string{"index", "value"}, gives: kVoid, run: func(m *Machine, _ float64, a, v []float64) float64 {
		a[m.index(v[0], len(a), len(a))] = v[1]
		return nan
	}},
	"push": {params: []string{"value"}, gives: kVoid, run: func(m *Machine, id float64, a, v []float64) float64 {
		store(m, id, append(a, v[0]))
		return nan
	}},
	"unshift": {params: []string{"value"}, gives: kVoid, run: func(m *Machine, id float64, a, v []float64) float64 {
		store(m, id, slices.Insert(a, 0, v[0]))
		return nan
	}},
	"insert": {params: []string{"index", "value"}, gives: kVoid, run: func(m *Machine, id float64, a, v []float64) float64 {
		store(m, id, slices.Insert(a, m.index(v[0], len(a), len(a)+1), v[1]))
		return nan
	}},
	"pop": {gives: kFloat, run: func(m *Machine, id float64, a, _ []float64) float64 {
		k := m.index(-1, len(a), len(a))
		store(m, id, a[:k])
		return a[k]
	}},
	"shift": {gives: kFloat, run: func(m *Machine, id float64, a, _ []float64) float64 {
		x := a[m.index(0, len(a), len(a))]
		store(m, id, slices.Delete(a, 0, 1))
		return x
	}},
	"remove": {params: []string{"index"}, gives: kFloat, run: func(m *Machine, id float64, a, v []float64) float64 {
		k := m.index(v[0], len(a), len(a))
		x := a[k]
		store(m, id, slices.Delete(a, k, k+1))
		return x
	}},
	"clear": {gives: kVoid, run: func(m *Machine, id float64, a, _ []float64) float64 {
		store(m, id, a[:0])
		return nan
	}},
	"first": {gives: kFloat, run: func(m *Machine, _ float64, a, _ []float64) float64 { return a[m.index(0, len(a), len(a))] }},
	"last":  {gives: kFloat, run: func(m *Machine, _ float64, a, _ []float64) float64 { return a[m.index(-1, len(a), len(a))] }},
	"indexof": {params: []string{"value"}, gives: kFloat, run: func(_ *Machine, _ float64, a, v []float64) float64 {
		return float64(slices.IndexFunc(a, func(x float64) bool { return eqNa(x, v[0]) }))
	}},
	"lastindexof": {params: []string{"value"}, gives: kFloat, run: func(_ *Machine, _ float64, a, v []float64) float64 {
		for k := len(a) - 1; k >= 0; k-- {
			if eqNa(a[k], v[0]) {
				return float64(k)
			}
		}
		return -1
	}},
	"includes": {params: []string{"value"}, gives: kFloat, run: func(_ *Machine, _ float64, a, v []float64) float64 {
		return boolF(slices.ContainsFunc(a, func(x float64) bool { return eqNa(x, v[0]) }))
	}},
	"min": {params: []string{"nth"}, gives: kFloat, run: func(_ *Machine, _ float64, a, v []float64) float64 {
		s, k := sorted(a), int(nzOr(v[0], 0))
		if k < 0 || k >= len(s) {
			return nan
		}
		return s[k]
	}},
	"max": {params: []string{"nth"}, gives: kFloat, run: func(_ *Machine, _ float64, a, v []float64) float64 {
		s, k := sorted(a), int(nzOr(v[0], 0))
		if k < 0 || k >= len(s) {
			return nan
		}
		return s[len(s)-1-k]
	}},
	"range": {gives: kFloat, run: func(_ *Machine, _ float64, a, _ []float64) float64 {
		if s := sorted(a); len(s) > 0 {
			return s[len(s)-1] - s[0]
		}
		return nan
	}},
	"sum": {gives: kFloat, run: func(_ *Machine, _ float64, a, _ []float64) float64 {
		t := 0.0
		for _, x := range numbers(a) {
			t += x
		}
		return t
	}},
	"avg": {gives: kFloat, run: func(_ *Machine, _ float64, a, _ []float64) float64 {
		s := numbers(a)
		if len(s) == 0 {
			return nan
		}
		t := 0.0
		for _, x := range s {
			t += x
		}
		return t / float64(len(s))
	}},
	"median": {gives: kFloat, run: func(_ *Machine, _ float64, a, _ []float64) float64 {
		s := sorted(a)
		if len(s) == 0 {
			return nan
		}
		if len(s)%2 == 1 {
			return s[len(s)/2]
		}
		return (s[len(s)/2-1] + s[len(s)/2]) / 2
	}},
	"variance": {params: []string{"biased"}, gives: kFloat, run: func(_ *Machine, _ float64, a, v []float64) float64 {
		return variance(a, nzOr(v[0], 1))
	}},
	"stdev": {params: []string{"biased"}, gives: kFloat, run: func(_ *Machine, _ float64, a, v []float64) float64 {
		return math.Sqrt(variance(a, nzOr(v[0], 1)))
	}},
	"reverse": {gives: kVoid, run: func(_ *Machine, _ float64, a, _ []float64) float64 {
		slices.Reverse(a)
		return nan
	}},
	"sort": {params: []string{"order"}, gives: kVoid, run: func(_ *Machine, _ float64, a, v []float64) float64 {
		desc := v[0] < 0
		slices.SortStableFunc(a, func(x, y float64) int {
			switch {
			case math.IsNaN(x) || math.IsNaN(y): // na last
				return boolInt(math.IsNaN(x)) - boolInt(math.IsNaN(y))
			case desc:
				return cmpF(y, x)
			}
			return cmpF(x, y)
		})
		return nan
	}},
	"fill": {params: []string{"value", "index_from", "index_to"}, gives: kVoid, run: func(m *Machine, _ float64, a, v []float64) float64 {
		from, to := 0, len(a)
		if !math.IsNaN(v[1]) {
			from = m.index(v[1], len(a), len(a)+1)
		}
		if !math.IsNaN(v[2]) {
			to = m.index(v[2], len(a), len(a)+1)
		}
		for k := from; k < to; k++ {
			a[k] = v[0]
		}
		return nan
	}},
	"copy": {gives: kFloat, newArr: true, run: func(m *Machine, _ float64, a, _ []float64) float64 {
		return m.newArray(slices.Clone(a))
	}},
	"concat": {params: []string{"id2"}, gives: kFloat, newArr: true, run: func(m *Machine, id float64, a, v []float64) float64 {
		store(m, id, append(a, m.array(v[0])...))
		return id
	}},
}

func nzOr(v, d float64) float64 {
	if math.IsNaN(v) {
		return d
	}
	return v
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func cmpF(x, y float64) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

// The element types an array.new_… can hold: every one but string is kept
// as numbers, drawings as na since OpenBar draws none.
var arrayElems = map[string]float64{"float": nan, "int": nan, "bool": 0, "color": nan,
	"line": nan, "label": nan, "box": nan, "linefill": nan, "table": nan, "polyline": nan}

func init() {
	builtinVars["order.ascending"] = func(*compiler) cx { return konstX(1) }
	builtinVars["order.descending"] = func(*compiler) cx { return konstX(-1) }
}

// arrayCall compiles array.name(…), or recv.name(…) for an array recv.
func (c *compiler) arrayCall(name string, recv *cx, x *callX, sc *scope) (cx, error) {
	c.s.arrays = true
	fn := "array." + name
	if elem, ok := strings.CutPrefix(name, "new_"); ok || name == "from" {
		if recv != nil {
			return cx{}, errAt(x.t, "OpenBar 不认识函数 %s", fn)
		}
		params := []string{"size", "initial_value"}
		def, known := arrayElems[elem]
		if name == "from" {
			params, def, known = []string{"value…"}, nan, true
		}
		if elem == "string" {
			return cx{}, errAt(x.t, "OpenBar 还不支持文字的数组")
		}
		if !known {
			return cx{}, errAt(x.t, "OpenBar 不认识函数 %s", fn)
		}
		a, err := c.arrayArgs(x, params, sc)
		if err != nil {
			return cx{}, err
		}
		out := numX(func(m *Machine) float64 {
			if name == "from" {
				v := make([]float64, len(a))
				for i, e := range a {
					v[i] = e.f(m)
				}
				return m.newArray(v)
			}
			n, init := 0.0, def
			if given(a[0]) {
				n = a[0].f(m)
			}
			if given(a[1]) {
				init = a[1].f(m)
			}
			if math.IsNaN(n) || n < 0 || n > maxArray {
				m.runErr("数组的大小 %v 不对", n)
			}
			v := make([]float64, int(n))
			for i := range v {
				v[i] = init
			}
			return m.newArray(v)
		})
		out.arr = true
		return out, nil
	}
	op, ok := arrOps[name]
	if !ok {
		return cx{}, errAt(x.t, "OpenBar 不认识函数 %s", fn)
	}
	params := op.params
	if recv == nil {
		params = append([]string{"id"}, params...)
	}
	a, err := c.arrayArgs(x, params, sc)
	if err != nil {
		return cx{}, err
	}
	if recv != nil {
		a = append([]cx{*recv}, a...)
	}
	if !given(a[0]) || !a[0].arr && !isNaX(a[0]) {
		return cx{}, errAt(x.t, "%s 的第一个参数要是数组", fn)
	}
	for i, p := range op.params {
		if (p == "id2") != a[i+1].arr && given(a[i+1]) && !isNaX(a[i+1]) {
			return cx{}, errAt(a[i+1].tok, "%s 的参数 %s 不对", fn, p)
		}
	}
	id, rest := a[0], a[1:]
	run := func(m *Machine) float64 {
		v := make([]float64, len(rest))
		for i, e := range rest {
			v[i] = nan
			if given(e) {
				v[i] = e.f(m)
			}
		}
		h := id.f(m)
		return op.run(m, h, m.array(h), v)
	}
	if op.gives == kVoid {
		return cx{k: kVoid, f: run}, nil
	}
	out := numX(run)
	out.arr = op.newArr
	return out, nil
}

// arrayArgs are a call's arguments, numbers all: strings have no arrays yet.
func (c *compiler) arrayArgs(x *callX, params []string, sc *scope) ([]cx, error) {
	a, err := c.args(x, params, sc)
	if err != nil {
		return nil, err
	}
	for _, v := range a {
		if v.k == kStr {
			return nil, errAt(v.tok, "OpenBar 还不支持文字的数组")
		}
		if given(v) && v.k != kFloat {
			return nil, errAt(v.tok, "%s 的参数要是数值", x.fn)
		}
	}
	return a, nil
}

// forIn compiles for x in a and for [i, x] in a.
func (c *compiler) forIn(st *forInS, sc *scope) (func(*Machine), error) {
	arr, err := c.expr(st.arr, sc)
	if err != nil {
		return nil, err
	}
	if !arr.arr {
		return nil, errAt(st.arr.at(), "for … in 后面要是数组")
	}
	inner := &scope{parent: sc, vars: map[string]*variable{}}
	idx := -1
	if st.idx != "" {
		idx = c.newBuf(false)
		inner.vars[st.idx] = &variable{k: kFloat, slot: idx, tok: st.t}
	}
	val := c.newBuf(false)
	inner.vars[st.v] = &variable{k: kFloat, slot: val, tok: st.t}
	c.local++
	c.loops++
	body, err := c.block(st.body, inner)
	c.local--
	c.loops--
	if err != nil {
		return nil, err
	}
	return func(m *Machine) {
		id := arr.f(m)
		for n := 0; ; n++ {
			a := m.array(id)
			if n >= len(a) {
				return
			}
			m.loop(n)
			if idx >= 0 {
				m.buf[idx][m.i] = float64(n)
			}
			m.buf[val][m.i] = a[n]
			runBlock(m, body)
			if m.jump == jumpBreak {
				m.jump = 0
				return
			}
			m.jump = 0
		}
	}, nil
}
