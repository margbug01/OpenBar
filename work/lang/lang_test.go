package lang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// says are the functions that say an argument in the language shown, by its
// index: T and F, and the helpers that hand theirs to T.
var says = map[string][]int{
	"T": {0}, "F": {0}, "errAt": {1}, "warn": {1}, "runErr": {0},
	"row": {1}, "checkRow": {2}, "section": {2}, "triCheck": {2}, "boolCheck": {2},
	"dialogHeader": {3, 4}, "dropdown": {1}, "intChoice": {1}, "well": {1},
	"legendButton": {4}, "toolButton": {4}, "barButton": {4, 5}, "cornerButton": {2, 3},
	"header": {2, 3}, "link": {1, 2}, "lineRow": {0}, "margin": {0, 3},
}

// Every Chinese text the code hands T, F or a helper saying it has its
// English, taking the same arguments.
func TestEnglishForEveryText(t *testing.T) {
	verbs := regexp.MustCompile(`%[-+# 0]*[0-9.*]*[a-zA-Z%]`)
	seen := 0
	filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "signalbot" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			var name string
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				name = fn.Name
			case *ast.SelectorExpr:
				name = fn.Sel.Name
				if x, ok := fn.X.(*ast.Ident); (name == "T" || name == "F") && (!ok || x.Name != "lang") {
					return true
				}
			}
			format := name == "F" || name == "errAt" || name == "warn" || name == "runErr"
			for _, i := range says[name] {
				if i >= len(call.Args) {
					continue
				}
				lit, ok := call.Args[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s, _ := strconv.Unquote(lit.Value)
				if !hasHan(s) {
					continue
				}
				seen++
				e, ok := en[s]
				switch {
				case !ok:
					t.Errorf("%s: no English for %q", fset.Position(lit.Pos()), s)
				case format && !slices.Equal(sortedVerbs(verbs, s), sortedVerbs(verbs, e)):
					t.Errorf("%s: %q and %q take different arguments", fset.Position(lit.Pos()), s, e)
				}
			}
			return true
		})
		return nil
	})
	if seen == 0 {
		t.Fatal("found no texts")
	}
}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// sortedVerbs are the verbs of a format, sorted: English may put them in
// another order only with explicit indexes, %[2]s, which this reads as one
// verb.
func sortedVerbs(re *regexp.Regexp, s string) []string {
	v := re.FindAllString(regexp.MustCompile(`\[\d+\]`).ReplaceAllString(s, ""), -1)
	slices.Sort(v)
	return v
}

func TestSet(t *testing.T) {
	defer Set("")
	for _, c := range []struct {
		tag  string
		want bool
	}{{"zh-Hans", false}, {"zh-Hant", false}, {"en", true}, {"", false}} {
		Set(c.tag)
		if English() != c.want {
			t.Errorf("Set(%q): English %v", c.tag, English())
		}
	}
	Set("en")
	if T("取消") != "Cancel" || T("比特币之外的名字") != "比特币之外的名字" {
		t.Errorf("T in English: %q, %q", T("取消"), T("比特币之外的名字"))
	}
}
